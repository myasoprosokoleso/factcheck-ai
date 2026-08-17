package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/myasoprosokoleso/factcheck-ai/internal/job"
	"github.com/myasoprosokoleso/factcheck-ai/internal/post"
)

const maxAttempts = 3

type jobRepository struct {
	pool *pgxpool.Pool
}

func NewJobRepository(pool *pgxpool.Pool) job.Repository {
	return &jobRepository{pool: pool}
}

func (r *jobRepository) EnqueueFactCheck(ctx context.Context, payload post.FactCheckPostPayload) (bool, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("encode fact-check job: %w", err)
	}
	return r.enqueue(ctx, job.EnqueueParams{
		Type:      job.TypeFactCheckPost,
		Payload:   encoded,
		DedupeKey: "factcheck:" + payload.PostID,
	})
}

func (r *jobRepository) enqueue(ctx context.Context, params job.EnqueueParams) (bool, error) {
	id, err := newUUID()
	if err != nil {
		return false, err
	}
	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	var storedID string
	err = r.pool.QueryRow(operationCtx, `
		INSERT INTO jobs (
			id, type, payload, status, dedupe_key
		)
		VALUES ($1, $2, $3, 'PENDING', $4)
		ON CONFLICT (type, dedupe_key) DO NOTHING
		RETURNING id::text
	`, id, params.Type, params.Payload, params.DedupeKey).Scan(&storedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("enqueue job: %w", err)
	}
	return true, nil
}

func (r *jobRepository) Claim(ctx context.Context, params job.ClaimParams) (*job.Job, error) {
	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	tx, err := r.pool.BeginTx(operationCtx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(operationCtx) }()

	var id string
	err = tx.QueryRow(operationCtx, `
		SELECT id::text
		FROM jobs
		WHERE status IN ('PENDING', 'RETRY')
			AND available_at <= now()
			AND type = $1
		ORDER BY created_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, params.Type).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if rollbackErr := tx.Rollback(operationCtx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return nil, fmt.Errorf("rollback empty claim transaction: %w", rollbackErr)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select claimable job: %w", err)
	}

	var job job.Job
	err = tx.QueryRow(operationCtx, `
		UPDATE jobs
		SET status = 'PROCESSING',
			attempts = attempts + 1,
			locked_at = now(),
			locked_by = $2,
			updated_at = now()
		WHERE id = $1
		RETURNING id::text, type, payload, attempts
	`, id, params.WorkerID).Scan(&job.ID, &job.Type, &job.Payload, &job.Attempts)
	if err != nil {
		return nil, fmt.Errorf("lock claimed job: %w", err)
	}
	if err := tx.Commit(operationCtx); err != nil {
		return nil, fmt.Errorf("commit claim transaction: %w", err)
	}
	return &job, nil
}

func (r *jobRepository) Complete(ctx context.Context, jobID, workerID string) error {
	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	commandTag, err := r.pool.Exec(operationCtx, `
		UPDATE jobs
		SET status = 'COMPLETED', locked_at = NULL, locked_by = NULL,
			last_error = NULL, updated_at = now()
		WHERE id = $1 AND status = 'PROCESSING' AND locked_by = $2
	`, jobID, workerID)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return job.ErrNotOwned
	}
	return nil
}

func (r *jobRepository) Fail(ctx context.Context, failure job.FailureParams) error {
	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	commandTag, err := r.pool.Exec(operationCtx, `
		UPDATE jobs
		SET status = CASE WHEN attempts >= $4 THEN 'DEAD' ELSE 'RETRY' END,
			available_at = CASE
				WHEN attempts >= $4 THEN available_at
				ELSE $5
			END,
			locked_at = NULL,
			locked_by = NULL,
			last_error = $3,
			updated_at = now()
		WHERE id = $1 AND status = 'PROCESSING' AND locked_by = $2
	`, failure.JobID, failure.WorkerID, truncateError(failure.Error), maxAttempts, failure.RetryAt)
	if err != nil {
		return fmt.Errorf("fail job: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return job.ErrNotOwned
	}
	return nil
}

func (r *jobRepository) Dead(ctx context.Context, jobID, workerID, reason string) error {
	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	commandTag, err := r.pool.Exec(operationCtx, `
		UPDATE jobs
		SET status = 'DEAD', locked_at = NULL, locked_by = NULL,
			last_error = $3, updated_at = now()
		WHERE id = $1 AND status = 'PROCESSING' AND locked_by = $2
	`, jobID, workerID, truncateError(reason))
	if err != nil {
		return fmt.Errorf("mark job dead: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return job.ErrNotOwned
	}
	return nil
}

// RequeueStale releases PROCESSING jobs whose worker lease predates
// lockedBefore. Callers choose a threshold longer than their maximum job time.
func (r *jobRepository) RequeueStale(ctx context.Context, lockedBefore time.Time) (int64, error) {
	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	commandTag, err := r.pool.Exec(operationCtx, `
		UPDATE jobs
		SET status = CASE WHEN attempts >= $2 THEN 'DEAD' ELSE 'RETRY' END,
			available_at = now(),
			locked_at = NULL,
			locked_by = NULL,
			last_error = 'worker lease expired',
			updated_at = now()
		WHERE status = 'PROCESSING' AND locked_at < $1
	`, lockedBefore, maxAttempts)
	if err != nil {
		return 0, fmt.Errorf("requeue stale jobs: %w", err)
	}
	return commandTag.RowsAffected(), nil
}

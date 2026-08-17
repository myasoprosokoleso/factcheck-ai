package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/myasoprosokoleso/factcheck-ai/internal/factcheck"
	"github.com/myasoprosokoleso/factcheck-ai/internal/job"
)

type factCheckRepository struct {
	pool *pgxpool.Pool
}

func NewFactCheckRepository(pool *pgxpool.Pool) factcheck.Repository {
	return &factCheckRepository{pool: pool}
}

func (r *factCheckRepository) SaveResult(ctx context.Context, postID string, result factcheck.Result) error {
	encodedResult, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode fact-check result: %w", err)
	}
	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	tx, err := r.pool.BeginTx(operationCtx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin fact-check transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(operationCtx) }()

	commandTag, err := tx.Exec(operationCtx, `
		INSERT INTO fact_checks (
			post_id, result
		)
		VALUES ($1, $2::jsonb)
		ON CONFLICT (post_id) DO NOTHING
	`, postID, string(encodedResult))
	if err != nil {
		return fmt.Errorf("insert fact-check result: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return nil
	}
	if !result.ShouldComment() {
		if err := tx.Commit(operationCtx); err != nil {
			return fmt.Errorf("commit fact-check result: %w", err)
		}
		return nil
	}

	payload, err := json.Marshal(job.PublishCommentPayload{
		PostID: postID,
	})
	if err != nil {
		return fmt.Errorf("encode publish job: %w", err)
	}

	jobID, err := newUUID()
	if err != nil {
		return err
	}

	if _, err := tx.Exec(operationCtx, `
		INSERT INTO jobs (
			id, type, payload, status, dedupe_key
		)
		VALUES ($1, $2, $3, 'PENDING', $4)
		ON CONFLICT (type, dedupe_key) DO NOTHING
	`, jobID, job.TypePublishComment, payload, "publish:"+postID); err != nil {
		return fmt.Errorf("enqueue publish comment: %w", err)
	}

	if err := tx.Commit(operationCtx); err != nil {
		return fmt.Errorf("commit fact-check result: %w", err)
	}
	return nil
}

func (r *factCheckRepository) ForDelivery(ctx context.Context, postID string) (factcheck.DeliveryWork, error) {
	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	var work factcheck.DeliveryWork
	err := r.pool.QueryRow(operationCtx, `
		SELECT post.id::text,
			channel.telegram_channel_id, channel.public_username,
			post.telegram_message_id, fact_check.result ->> 'comment'
		FROM fact_checks AS fact_check
		JOIN posts AS post ON post.id = fact_check.post_id
		JOIN channels AS channel ON channel.telegram_channel_id = post.telegram_channel_id
		WHERE fact_check.post_id = $1
	`, postID).Scan(
		&work.PostID,
		&work.TelegramChannelID,
		&work.PublicUsername,
		&work.TelegramMessageID,
		&work.CommentText,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return factcheck.DeliveryWork{}, factcheck.ErrNotFound
	}
	if err != nil {
		return factcheck.DeliveryWork{}, fmt.Errorf("load fact-check for delivery: %w", err)
	}
	return work, nil
}

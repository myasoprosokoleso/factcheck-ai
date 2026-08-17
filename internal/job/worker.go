package job

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	rand "math/rand/v2"
	"time"
)

type Handler func(context.Context, Job) error

// RetryPolicy implements min(base*2^attempt, max)+jitter. attempt is zero
// based: the first retry uses BaseDelay.
type RetryPolicy struct {
	BaseDelay time.Duration
	MaxDelay  time.Duration
	Jitter    time.Duration
}

type retryAfterError struct {
	err   error
	delay time.Duration
}

func (e retryAfterError) Error() string { return e.err.Error() }
func (e retryAfterError) Unwrap() error { return e.err }

// RetryAfter marks an error for retry after at least delay. The worker adds its
// configured jitter but does not apply exponential backoff.
func RetryAfter(err error, delay time.Duration) error {
	return retryAfterError{err: err, delay: delay}
}

func retryDelay(err error) (time.Duration, bool) {
	if delayed, ok := errors.AsType[retryAfterError](err); ok {
		return delayed.delay, true
	}
	return 0, false
}

func (p RetryPolicy) delay(attempt int) time.Duration {
	delay := p.BaseDelay
	for range attempt {
		if delay >= p.MaxDelay || delay > p.MaxDelay/2 {
			delay = p.MaxDelay
			break
		}
		delay *= 2
	}
	return delay + p.jitter()
}

func (p RetryPolicy) delayForError(err error, attempt int) time.Duration {
	if providerDelay, ok := retryDelay(err); ok {
		return providerDelay + p.jitter()
	}
	return p.delay(attempt)
}

func (p RetryPolicy) jitter() time.Duration {
	return time.Duration(rand.Float64() * float64(p.Jitter))
}

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

func Permanent(err error) error {
	return permanentError{err: err}
}

func isPermanent(err error) bool {
	_, ok := errors.AsType[permanentError](err)
	return ok
}

type Worker struct {
	repository   Repository
	workerID     string
	jobType      Type
	handler      Handler
	retryPolicy  RetryPolicy
	pollInterval time.Duration
	jobTimeout   time.Duration
	logger       *slog.Logger
}

type WorkerConfig struct {
	Repository   Repository
	WorkerID     string
	Type         Type
	Handler      Handler
	RetryPolicy  RetryPolicy
	PollInterval time.Duration
	JobTimeout   time.Duration
	Logger       *slog.Logger
}

func NewWorker(config WorkerConfig) *Worker {
	return &Worker{
		repository:   config.Repository,
		workerID:     config.WorkerID,
		jobType:      config.Type,
		handler:      config.Handler,
		retryPolicy:  config.RetryPolicy,
		pollInterval: config.PollInterval,
		jobTimeout:   config.JobTimeout,
		logger:       config.Logger,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	for {
		processed, err := w.runOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.logger.ErrorContext(ctx, "job queue operation failed", "worker_id", w.workerID, "error", err)
		}
		if processed && err == nil {
			continue
		}

		timer := time.NewTimer(w.pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func (w *Worker) runOnce(ctx context.Context) (bool, error) {
	job, err := w.repository.Claim(ctx, ClaimParams{
		WorkerID: w.workerID,
		Type:     w.jobType,
	})
	if err != nil {
		return false, fmt.Errorf("claim job: %w", err)
	}
	if job == nil {
		return false, nil
	}

	processingCtx, cancel := context.WithTimeout(ctx, w.jobTimeout)
	err = w.handler(processingCtx, *job)
	cancel()

	if err == nil {
		if completeErr := w.repository.Complete(ctx, job.ID, w.workerID); completeErr != nil {
			return true, fmt.Errorf("complete job: %w", completeErr)
		}
		return true, nil
	}

	if isPermanent(err) {
		if deadErr := w.repository.Dead(ctx, job.ID, w.workerID, err.Error()); deadErr != nil {
			return true, fmt.Errorf("mark job dead: %w", deadErr)
		}
		return true, nil
	}

	// Claim increments Attempts, hence attempts-1 is the zero-based retry
	// number required by RetryPolicy.
	retryAt := time.Now().UTC().Add(w.retryPolicy.delayForError(err, job.Attempts-1))
	if failErr := w.repository.Fail(ctx, FailureParams{
		JobID:    job.ID,
		WorkerID: w.workerID,
		Error:    err.Error(),
		RetryAt:  retryAt,
	}); failErr != nil {
		return true, fmt.Errorf("fail job: %w", failErr)
	}
	return true, nil
}

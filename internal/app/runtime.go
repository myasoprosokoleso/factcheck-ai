package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/myasoprosokoleso/factcheck-ai/internal/job"
	"github.com/myasoprosokoleso/factcheck-ai/internal/observability"
)

type component struct {
	name string
	run  func(context.Context) error
}

func runComponents(ctx context.Context, health *observability.HealthServer, components ...component) error {
	group, runCtx := errgroup.WithContext(ctx)
	for _, component := range components {
		group.Go(func() error {
			err := component.run(runCtx)
			if err == nil && runCtx.Err() == nil {
				return fmt.Errorf("%s stopped unexpectedly", component.name)
			}
			return err
		})
	}
	group.Go(func() error {
		err := health.Run()
		if err == nil && runCtx.Err() == nil {
			return errors.New("health server stopped unexpectedly")
		}
		return err
	})
	group.Go(func() error {
		<-runCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return health.Shutdown(shutdownCtx)
	})

	err := group.Wait()
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

func recoverStaleJobs(
	ctx context.Context,
	jobs job.Repository,
	lease time.Duration,
	logger *slog.Logger,
) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			count, err := jobs.RequeueStale(ctx, now.UTC().Add(-lease))
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				logger.WarnContext(ctx, "stale job recovery failed", "error", err)
				continue
			}
			if count > 0 {
				logger.InfoContext(ctx, "requeued stale jobs", "count", count)
			}
		}
	}
}

func workerID(prefix string) string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s-%s-%d", prefix, hostname, os.Getpid())
}

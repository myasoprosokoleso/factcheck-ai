package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/myasoprosokoleso/factcheck-ai/internal/channel"
	"github.com/myasoprosokoleso/factcheck-ai/internal/config"
	"github.com/myasoprosokoleso/factcheck-ai/internal/factcheck"
	"github.com/myasoprosokoleso/factcheck-ai/internal/job"
	"github.com/myasoprosokoleso/factcheck-ai/internal/observability"
	"github.com/myasoprosokoleso/factcheck-ai/internal/openai"
	"github.com/myasoprosokoleso/factcheck-ai/internal/postgres"
	"github.com/myasoprosokoleso/factcheck-ai/internal/telegram"
)

const factCheckJobTimeout = 90 * time.Second

func runServe(ctx context.Context, cfg config.Config) error {
	if err := cfg.ValidateServe(); err != nil {
		return err
	}

	logger := observability.NewLogger(os.Stdout, cfg.LogLevel)
	logger.Info("starting factcheck", "config", cfg.LogValues())

	pool, err := postgres.OpenPool(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer pool.Close()

	channels := postgres.NewChannelRepository(pool)
	posts := postgres.NewPostRepository(pool)
	jobs := postgres.NewJobRepository(pool)
	factChecks := postgres.NewFactCheckRepository(pool)
	metrics := observability.NewMetrics()

	factCheckService, err := newFactCheckService(cfg)
	if err != nil {
		return err
	}

	factCheckWorker := job.NewWorker(job.WorkerConfig{
		Repository: jobs,
		WorkerID:   workerID("factcheck"),
		Type:       job.TypeFactCheckPost,
		Handler:    factCheckHandler(factCheckService, posts, factChecks, metrics),
		RetryPolicy: job.RetryPolicy{
			BaseDelay: 2 * time.Second,
			MaxDelay:  5 * time.Minute,
			Jitter:    time.Second,
		},
		PollInterval: time.Second,
		JobTimeout:   factCheckJobTimeout,
		Logger:       logger,
	})

	updateHandler := &telegram.Handler{
		OwnerUserID: cfg.Telegram.OwnerUserID,
		Channels:    channels,
		Posts:       posts,
		Jobs:        jobs,
	}
	client, err := telegram.NewClient(telegram.ClientConfig{
		APIID:          cfg.Telegram.APIID,
		APIHash:        cfg.Telegram.APIHash,
		SessionPath:    cfg.Telegram.SessionPath,
		RequestTimeout: 15 * time.Second,
	}, measuredUpdateHandler{next: updateHandler, metrics: metrics})
	if err != nil {
		return fmt.Errorf("configure Telegram client: %w", err)
	}

	updateHandler.Commands = &telegram.CommandHandler{Channels: channel.NewService(client, channels)}
	updateHandler.Client = client

	publishWorker := job.NewWorker(job.WorkerConfig{
		Repository: jobs,
		WorkerID:   workerID("telegram"),
		Type:       job.TypePublishComment,
		Handler:    publishCommentHandler(factChecks, client, metrics),
		RetryPolicy: job.RetryPolicy{
			BaseDelay: 5 * time.Second,
			MaxDelay:  10 * time.Minute,
			Jitter:    3 * time.Second,
		},
		PollInterval: time.Second,
		JobTimeout:   3 * time.Minute,
		Logger:       logger,
	})

	health := observability.NewHealthServer(cfg.HTTPAddr, metrics.Handler())
	err = runComponents(ctx, health,
		component{name: "MTProto updates", run: client.Run},
		component{name: "fact-check queue", run: factCheckWorker.Run},
		component{name: "comment publisher", run: func(runCtx context.Context) error {
			select {
			case <-runCtx.Done():
				return nil
			case <-client.Ready():
			}
			health.SetReady(true)
			defer health.SetReady(false)
			return publishWorker.Run(runCtx)
		}},
		component{name: "stale job recovery", run: func(runCtx context.Context) error {
			return recoverStaleJobs(runCtx, jobs, 10*time.Minute, logger)
		}},
	)
	health.SetReady(false)
	return err
}

func newFactCheckService(cfg config.Config) (*factcheck.Service, error) {
	openAIClient, err := openai.New(cfg.OpenAI)
	if err != nil {
		return nil, fmt.Errorf("configure OpenAI client: %w", err)
	}
	return factcheck.NewService(openAIClient), nil
}

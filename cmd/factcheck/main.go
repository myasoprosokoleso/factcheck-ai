package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/myasoprosokoleso/factcheck-ai/internal/app"
	"github.com/myasoprosokoleso/factcheck-ai/internal/observability"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, os.Args[1:]); err != nil {
		logger := observability.NewLogger(os.Stderr, "info")
		logger.Error("factcheck stopped", "error", err)
		os.Exit(1)
	}
}

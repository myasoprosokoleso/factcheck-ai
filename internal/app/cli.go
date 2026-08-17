package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/myasoprosokoleso/factcheck-ai/internal/config"
	"github.com/myasoprosokoleso/factcheck-ai/internal/postgres"
	"github.com/myasoprosokoleso/factcheck-ai/internal/telegram"
)

const usageMessage = "usage: factcheck {serve|telegram login|migrate up|migrate down}"

func Run(ctx context.Context, args []string) error {
	if len(args) == 1 {
		switch args[0] {
		case "help", "-h", "--help":
			fmt.Println(usageMessage)
			return nil
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	switch {
	case len(args) == 1 && args[0] == "serve":
		return runServe(ctx, cfg)
	case len(args) == 2 && args[0] == "telegram" && args[1] == "login":
		return runTelegramLogin(ctx, cfg)
	case len(args) == 2 && args[0] == "migrate" && (args[1] == "up" || args[1] == "down"):
		return postgres.RunMigrations(ctx, cfg, args[1])
	default:
		return errors.New(usageMessage)
	}
}

func runTelegramLogin(ctx context.Context, cfg config.Config) error {
	if err := cfg.ValidateTelegramLogin(); err != nil {
		return err
	}

	if err := telegram.InteractiveLogin(ctx, cfg.Telegram); err != nil {
		return err
	}

	fmt.Println("Telegram login completed")
	return nil
}

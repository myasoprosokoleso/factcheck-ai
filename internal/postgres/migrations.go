package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/myasoprosokoleso/factcheck-ai/internal/config"
)

const (
	migrationLockID  = int64(67)
	migrationTimeout = 2 * time.Minute
	cleanupTimeout   = 5 * time.Second
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func RunMigrations(ctx context.Context, cfg config.Config, direction string) error {
	if err := cfg.ValidateMigrations(); err != nil {
		return err
	}

	connectionConfig, err := pgx.ParseConfig(cfg.PostgresDSN)
	if err != nil {
		return errors.New("parse PostgreSQL configuration")
	}
	db := stdlib.OpenDB(*connectionConfig)
	defer db.Close()

	operationCtx, cancel := context.WithTimeout(ctx, migrationTimeout)
	defer cancel()
	if err := db.PingContext(operationCtx); err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}

	migrationLocker, err := lock.NewPostgresSessionLocker(
		lock.WithLockID(migrationLockID),
		lock.WithLockTimeout(1, uint64(migrationTimeout/time.Second)),
		lock.WithUnlockTimeout(1, uint64(cleanupTimeout/time.Second)),
	)
	if err != nil {
		return fmt.Errorf("configure migration lock: %w", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrationFS,
		goose.WithLogger(goose.NopLogger()),
		goose.WithSessionLocker(migrationLocker),
	)
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	switch direction {
	case "up":
		return migrateUp(operationCtx, provider)
	default: // direction has already been validated in app.Run
		return migrateDown(operationCtx, provider)
	}
}

func migrateUp(ctx context.Context, provider *goose.Provider) error {
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("migrations are up to date")
		return nil
	}
	for _, result := range results {
		fmt.Printf("applied migration %s\n", migrationName(result))
	}
	return nil
}

func migrateDown(ctx context.Context, provider *goose.Provider) error {
	result, err := provider.Down(ctx)
	if errors.Is(err, goose.ErrNoNextVersion) {
		fmt.Println("no migrations to roll back")
		return nil
	}
	if err != nil {
		return fmt.Errorf("roll back migration: %w", err)
	}

	fmt.Printf("rolled back migration %s\n", migrationName(result))
	return nil
}

func migrationName(result *goose.MigrationResult) string {
	if result == nil || result.Source == nil {
		return "<unknown>"
	}
	return filepath.Base(result.Source.Path)
}

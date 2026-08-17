package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/myasoprosokoleso/factcheck-ai/internal/channel"
)

type channelRepository struct {
	pool *pgxpool.Pool
}

func NewChannelRepository(pool *pgxpool.Pool) channel.Repository {
	return &channelRepository{pool: pool}
}

func (r *channelRepository) Add(ctx context.Context, ch channel.Channel) (channel.Channel, error) {
	var lastError any
	if ch.LastError != "" {
		lastError = truncateError(ch.LastError)
	}

	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	stored, err := scanChannel(r.pool.QueryRow(operationCtx, `
		INSERT INTO channels (
			telegram_channel_id, public_username, status, last_error
		)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (telegram_channel_id) DO UPDATE SET
			public_username = EXCLUDED.public_username,
			status = EXCLUDED.status,
			last_error = EXCLUDED.last_error,
			updated_at = now()
		RETURNING telegram_channel_id, public_username, status,
			COALESCE(last_error, '')
	`, ch.TelegramID, ch.PublicUsername, ch.Status, lastError))
	if err != nil {
		return channel.Channel{}, fmt.Errorf("save channel: %w", err)
	}
	return stored, nil
}

func (r *channelRepository) ChannelByTelegramID(ctx context.Context, telegramID int64) (channel.Channel, error) {
	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	ch, err := scanChannel(r.pool.QueryRow(operationCtx, `
		SELECT telegram_channel_id, public_username, status,
			COALESCE(last_error, '')
		FROM channels
		WHERE telegram_channel_id = $1
	`, telegramID))
	if errors.Is(err, pgx.ErrNoRows) {
		return channel.Channel{}, channel.ErrNotFound
	}
	if err != nil {
		return channel.Channel{}, fmt.Errorf("get channel by telegram id: %w", err)
	}
	return ch, nil
}

func (r *channelRepository) List(ctx context.Context) ([]channel.Channel, error) {
	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(operationCtx, `
		SELECT telegram_channel_id, public_username, status,
			COALESCE(last_error, '')
		FROM channels
		ORDER BY created_at, telegram_channel_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()

	channels := make([]channel.Channel, 0)
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channels: %w", err)
	}
	return channels, nil
}

func (r *channelRepository) Delete(ctx context.Context, publicUsername string) error {
	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	commandTag, err := r.pool.Exec(operationCtx, `
		DELETE FROM channels
		WHERE public_username = $1
	`, publicUsername)
	if err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return channel.ErrNotFound
	}
	return nil
}

func scanChannel(row pgx.Row) (channel.Channel, error) {
	var ch channel.Channel
	if err := row.Scan(
		&ch.TelegramID,
		&ch.PublicUsername,
		&ch.Status,
		&ch.LastError,
	); err != nil {
		return channel.Channel{}, err
	}
	return ch, nil
}

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/myasoprosokoleso/factcheck-ai/internal/post"
)

type postRepository struct {
	pool *pgxpool.Pool
}

func NewPostRepository(pool *pgxpool.Pool) post.Repository {
	return &postRepository{pool: pool}
}

func (r *postRepository) Store(ctx context.Context, p post.Post) (string, error) {
	postID, err := newUUID()
	if err != nil {
		return "", err
	}

	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	var storedPostID string
	err = r.pool.QueryRow(operationCtx, `
		INSERT INTO posts (
			id, telegram_channel_id, telegram_message_id, published_at, text
		)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (telegram_channel_id, telegram_message_id) DO UPDATE SET
			published_at = LEAST(posts.published_at, EXCLUDED.published_at)
		RETURNING id::text
	`, postID, p.TelegramChannelID, p.TelegramMessageID,
		p.PublishedAt, p.Text).Scan(&storedPostID)
	if err != nil {
		return "", fmt.Errorf("upsert post: %w", err)
	}
	return storedPostID, nil
}

func (r *postRepository) TextByID(ctx context.Context, id string) (string, error) {
	operationCtx, cancel := withOperationTimeout(ctx)
	defer cancel()

	var text string
	err := r.pool.QueryRow(operationCtx, `
		SELECT text
		FROM posts
		WHERE id = $1
	`, id).Scan(&text)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", post.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get post text: %w", err)
	}
	return text, nil
}

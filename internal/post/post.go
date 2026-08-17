package post

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("post not found")
)

type Post struct {
	TelegramChannelID int64
	TelegramMessageID int64
	PublishedAt       time.Time
	Text              string
}

// Repository avoids a cyclical postgres package import
type Repository interface {
	Store(context.Context, Post) (string, error)
	TextByID(context.Context, string) (string, error)
}

type FactCheckPostPayload struct {
	PostID string `json:"post_id"`
}

func NormalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	normalized := make([]string, 0, len(lines))
	previousBlank := true
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if !previousBlank {
				normalized = append(normalized, "")
				previousBlank = true
			}
			continue
		}
		normalized = append(normalized, line)
		previousBlank = false
	}

	for len(normalized) > 0 && normalized[len(normalized)-1] == "" {
		normalized = normalized[:len(normalized)-1]
	}
	return strings.Join(normalized, "\n")
}

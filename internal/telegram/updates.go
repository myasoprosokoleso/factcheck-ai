package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/myasoprosokoleso/factcheck-ai/internal/channel"
	"github.com/myasoprosokoleso/factcheck-ai/internal/job"
	"github.com/myasoprosokoleso/factcheck-ai/internal/post"
)

type PostUpdate struct {
	ChannelID   int64
	MessageID   int64
	Text        string
	PublishedAt time.Time
}

type PrivateMessage struct {
	MessageID  int64
	SenderID   int64
	AccessHash int64
	Text       string
}

type updateHandler interface {
	HandlePost(context.Context, PostUpdate) error
	HandlePrivateMessage(context.Context, PrivateMessage) error
}

type Handler struct {
	OwnerUserID int64
	Channels    channel.Repository
	Posts       post.Repository
	Jobs        job.Repository
	Commands    *CommandHandler
	Client      *Client
}

func (h *Handler) HandlePost(ctx context.Context, update PostUpdate) error {
	if update.MessageID <= 0 {
		return nil
	}

	ch, err := h.Channels.ChannelByTelegramID(ctx, update.ChannelID)
	if errors.Is(err, channel.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lookup channel %d: %w", update.ChannelID, err)
	}
	if ch.Status != channel.StatusActive {
		return nil
	}

	text := post.NormalizeText(update.Text)
	if text == "" {
		return nil
	}
	publishedAt := update.PublishedAt.UTC()

	postID, err := h.Posts.Store(ctx, post.Post{
		TelegramChannelID: ch.TelegramID,
		TelegramMessageID: update.MessageID,
		PublishedAt:       publishedAt,
		Text:              text,
	})
	if err != nil {
		return fmt.Errorf("store telegram post: %w", err)
	}
	// Always attempt the idempotent enqueue, including for a duplicate update.
	// Otherwise a crash between Store and EnqueueFactCheck could lose work.
	_, err = h.Jobs.EnqueueFactCheck(ctx, post.FactCheckPostPayload{
		PostID: postID,
	})
	if err != nil {
		return fmt.Errorf("enqueue fact-check for post %s: %w", postID, err)
	}
	return nil
}

func (h *Handler) HandlePrivateMessage(ctx context.Context, message PrivateMessage) error {
	if message.SenderID != h.OwnerUserID || !strings.HasPrefix(strings.TrimSpace(message.Text), "/") {
		return nil
	}

	response, commandErr := h.Commands.handle(ctx, message.Text)
	if commandErr != nil {
		response = "Command failed: " + commandErr.Error()
	}

	sendErr := h.Client.sendPrivateMessage(
		ctx,
		message.SenderID,
		message.AccessHash,
		message.MessageID,
		response,
	)
	return errors.Join(commandErr, sendErr)
}

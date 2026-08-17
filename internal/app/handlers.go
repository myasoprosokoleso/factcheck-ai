package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/myasoprosokoleso/factcheck-ai/internal/channel"
	"github.com/myasoprosokoleso/factcheck-ai/internal/factcheck"
	"github.com/myasoprosokoleso/factcheck-ai/internal/job"
	"github.com/myasoprosokoleso/factcheck-ai/internal/observability"
	"github.com/myasoprosokoleso/factcheck-ai/internal/post"
	"github.com/myasoprosokoleso/factcheck-ai/internal/telegram"
)

func factCheckHandler(
	service *factcheck.Service,
	posts post.Repository,
	factChecks factcheck.Repository,
	metrics *observability.Metrics,
) job.Handler {
	return func(ctx context.Context, j job.Job) error {
		started := time.Now()
		outcome := "completed"
		defer func() {
			metrics.FactCheckJobsTotal.WithLabelValues(outcome).Inc()
			metrics.FactCheckDurationSeconds.Observe(time.Since(started).Seconds())
		}()

		var payload post.FactCheckPostPayload
		if err := json.Unmarshal(j.Payload, &payload); err != nil {
			outcome = "dead"
			return job.Permanent(errors.New("invalid FACTCHECK_POST payload"))
		}

		text, err := posts.TextByID(ctx, payload.PostID)
		if errors.Is(err, post.ErrNotFound) {
			outcome = "dead"
			return job.Permanent(err)
		}
		if err != nil {
			outcome = "retry"
			return fmt.Errorf("load post: %w", err)
		}

		result, err := service.Check(ctx, text)
		if err != nil {
			outcome = "retry"
			return fmt.Errorf("fact-check post: %w", err)
		}
		if err := factChecks.SaveResult(ctx, payload.PostID, result); err != nil {
			outcome = "retry"
			return fmt.Errorf("save fact-check result: %w", err)
		}

		metrics.FactCheckOutcomeTotal.WithLabelValues(string(result.Outcome)).Inc()
		return nil
	}
}

func publishCommentHandler(
	factChecks factcheck.Repository,
	client *telegram.Client,
	metrics *observability.Metrics,
) job.Handler {
	return func(ctx context.Context, j job.Job) error {
		var payload job.PublishCommentPayload
		if err := json.Unmarshal(j.Payload, &payload); err != nil {
			return job.Permanent(errors.New("invalid PUBLISH_COMMENT payload"))
		}

		work, err := factChecks.ForDelivery(ctx, payload.PostID)
		if errors.Is(err, factcheck.ErrNotFound) {
			return job.Permanent(err)
		}
		if err != nil {
			return err
		}
		resolvedChannel, err := client.ResolveChannel(ctx, work.PublicUsername)
		if err != nil {
			if errors.Is(err, channel.ErrNotFound) {
				return job.Permanent(err)
			}
			return fmt.Errorf("resolve source channel: %w", err)
		}
		if resolvedChannel.ID != work.TelegramChannelID {
			return job.Permanent(errors.New("stored Telegram channel ID changed"))
		}

		discussion, err := client.ResolveDiscussion(ctx, resolvedChannel)
		if err != nil {
			if errors.Is(err, channel.ErrDiscussionUnavailable) ||
				errors.Is(err, channel.ErrDeliveryUnavailable) {
				return job.Permanent(err)
			}
			return fmt.Errorf("resolve discussion: %w", err)
		}
		err = client.PublishComment(ctx, telegram.PublishCommentRequest{
			PostID:            work.PostID,
			Channel:           resolvedChannel,
			Discussion:        discussion,
			TelegramMessageID: work.TelegramMessageID,
			Text:              work.CommentText,
		})
		if err != nil {
			status := "error"
			floodWait, isFloodWait := errors.AsType[*telegram.FloodWaitError](err)
			if isFloodWait {
				status = "flood_wait"
			}
			metrics.TelegramCommentsTotal.WithLabelValues(status).Inc()
			if errors.Is(err, channel.ErrDiscussionUnavailable) ||
				errors.Is(err, channel.ErrDeliveryUnavailable) {
				return job.Permanent(err)
			}
			if isFloodWait {
				return job.RetryAfter(err, floodWait.Wait)
			}
			return err
		}
		metrics.TelegramCommentsTotal.WithLabelValues("published").Inc()
		return nil
	}
}

type measuredUpdateHandler struct {
	next    *telegram.Handler
	metrics *observability.Metrics
}

func (handler measuredUpdateHandler) HandlePost(ctx context.Context, update telegram.PostUpdate) error {
	handler.metrics.TelegramUpdatesTotal.WithLabelValues("new_post").Inc()
	return handler.next.HandlePost(ctx, update)
}

func (handler measuredUpdateHandler) HandlePrivateMessage(ctx context.Context, message telegram.PrivateMessage) error {
	handler.metrics.TelegramUpdatesTotal.WithLabelValues("private_message").Inc()
	return handler.next.HandlePrivateMessage(ctx, message)
}

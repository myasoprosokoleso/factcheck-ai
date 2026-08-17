package integration_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/myasoprosokoleso/factcheck-ai/internal/channel"
	"github.com/myasoprosokoleso/factcheck-ai/internal/job"
	"github.com/myasoprosokoleso/factcheck-ai/internal/post"
	"github.com/myasoprosokoleso/factcheck-ai/internal/telegram"
)

func TestTelegramIngestFlow(t *testing.T) {
	store := &telegramFlowStore{channel: channel.Channel{
		TelegramID:     100,
		PublicUsername: "news_channel",
		Status:         channel.StatusActive,
	}}
	handler := &telegram.Handler{Channels: store, Posts: store, Jobs: store}
	publishedAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.FixedZone("test", 3*60*60))
	incoming := telegram.PostUpdate{
		ChannelID:   100,
		MessageID:   55,
		Text:        "  Важный   факт\r\n\r\n  Источник: example.org  ",
		PublishedAt: publishedAt,
	}
	err := handler.HandlePost(t.Context(), incoming)
	if err != nil {
		t.Fatalf("HandlePost() error = %v", err)
	}

	const normalized = "Важный факт\n\nИсточник: example.org"
	if store.stored.TelegramChannelID != 100 || store.stored.Text != normalized {
		t.Fatalf("unexpected stored post: %+v", store.stored)
	}
	if !store.stored.PublishedAt.Equal(publishedAt.UTC()) {
		t.Fatalf("post metadata was not normalized: %+v", store.stored)
	}
	if store.job.PostID != "post-1" {
		t.Fatalf("unexpected queued job: %+v", store.job)
	}
	if store.createdPosts != 1 || store.createdJobs != 1 {
		t.Fatalf("created posts/jobs = %d/%d, want 1/1", store.createdPosts, store.createdJobs)
	}

	if err := handler.HandlePost(t.Context(), incoming); err != nil {
		t.Fatalf("HandlePost(duplicate) error = %v", err)
	}
	if store.createdPosts != 1 || store.createdJobs != 1 || store.storeCalls != 2 || store.enqueueCalls != 2 {
		t.Fatalf("duplicate created work: posts/jobs=%d/%d store/enqueue calls=%d/%d",
			store.createdPosts, store.createdJobs, store.storeCalls, store.enqueueCalls)
	}

	randomID := telegram.StableRandomID(store.job.PostID)
	if randomID == 0 || randomID != telegram.StableRandomID(store.job.PostID) {
		t.Fatalf("random_id = %d, want stable non-zero post ID", randomID)
	}
}

type telegramFlowStore struct {
	channel      channel.Channel
	stored       post.Post
	job          post.FactCheckPostPayload
	storeCalls   int
	createdPosts int
	enqueueCalls int
	createdJobs  int
	posts        map[string]string
	jobKeys      map[string]struct{}
}

func (store *telegramFlowStore) ChannelByTelegramID(_ context.Context, id int64) (channel.Channel, error) {
	if id != store.channel.TelegramID {
		return channel.Channel{}, channel.ErrNotFound
	}
	return store.channel, nil
}

func (store *telegramFlowStore) Add(context.Context, channel.Channel) (channel.Channel, error) {
	panic("unexpected Add call")
}

func (store *telegramFlowStore) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func (store *telegramFlowStore) List(context.Context) ([]channel.Channel, error) {
	panic("unexpected List call")
}

func (store *telegramFlowStore) Store(_ context.Context, sourcePost post.Post) (string, error) {
	store.storeCalls++
	store.stored = sourcePost
	if store.posts == nil {
		store.posts = make(map[string]string)
	}
	key := fmt.Sprintf("%d:%d", sourcePost.TelegramChannelID, sourcePost.TelegramMessageID)
	if postID, ok := store.posts[key]; ok {
		return postID, nil
	}
	store.createdPosts++
	postID := fmt.Sprintf("post-%d", store.createdPosts)
	store.posts[key] = postID
	return postID, nil
}

func (store *telegramFlowStore) TextByID(context.Context, string) (string, error) {
	panic("unexpected TextByID call")
}

func (store *telegramFlowStore) EnqueueFactCheck(_ context.Context, job post.FactCheckPostPayload) (bool, error) {
	store.enqueueCalls++
	store.job = job
	if store.jobKeys == nil {
		store.jobKeys = make(map[string]struct{})
	}
	key := "factcheck:" + job.PostID
	if _, ok := store.jobKeys[key]; ok {
		return false, nil
	}
	store.jobKeys[key] = struct{}{}
	store.createdJobs++
	return true, nil
}

func (store *telegramFlowStore) Claim(context.Context, job.ClaimParams) (*job.Job, error) {
	panic("unexpected Claim call")
}

func (store *telegramFlowStore) Complete(context.Context, string, string) error {
	panic("unexpected Complete call")
}

func (store *telegramFlowStore) Fail(context.Context, job.FailureParams) error {
	panic("unexpected Fail call")
}

func (store *telegramFlowStore) Dead(context.Context, string, string, string) error {
	panic("unexpected Dead call")
}

func (store *telegramFlowStore) RequeueStale(context.Context, time.Time) (int64, error) {
	panic("unexpected RequeueStale call")
}

package job

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/myasoprosokoleso/factcheck-ai/internal/post"
)

var ErrNotOwned = errors.New("job is not processing or is owned by another worker")

type Type string

const (
	TypeFactCheckPost  Type = "FACTCHECK_POST"
	TypePublishComment Type = "PUBLISH_COMMENT"
)

type Job struct {
	ID       string
	Type     Type
	Payload  json.RawMessage
	Attempts int
}

type PublishCommentPayload struct {
	PostID string `json:"post_id"`
}

// Repository avoids a cyclical postgres package import
type Repository interface {
	EnqueueFactCheck(context.Context, post.FactCheckPostPayload) (bool, error)
	Claim(context.Context, ClaimParams) (*Job, error)
	Complete(context.Context, string, string) error
	Fail(context.Context, FailureParams) error
	Dead(context.Context, string, string, string) error
	RequeueStale(context.Context, time.Time) (int64, error)
}

type EnqueueParams struct {
	Type      Type
	Payload   json.RawMessage
	DedupeKey string
}

type ClaimParams struct {
	WorkerID string
	Type     Type
}

type FailureParams struct {
	JobID    string
	WorkerID string
	Error    string
	RetryAt  time.Time
}

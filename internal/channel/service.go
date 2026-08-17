package channel

import (
	"context"
	"errors"
	"fmt"
)

// gateway avoids a cyclical telegram package import
type gateway interface {
	ResolveChannel(context.Context, string) (Resolved, error)
	JoinChannel(context.Context, Resolved) error
	ResolveDiscussion(context.Context, Resolved) (Discussion, error)
}

type AddResult struct {
	Channel              Channel
	DiscussionFound      bool
	CommentDeliveryReady bool
}

type Service struct {
	gateway gateway
	repo    Repository
}

func NewService(gateway gateway, repo Repository) *Service {
	return &Service{gateway: gateway, repo: repo}
}

func (s *Service) Add(ctx context.Context, publicUsername string) (AddResult, error) {
	resolved, err := s.gateway.ResolveChannel(ctx, publicUsername)
	if err != nil {
		return AddResult{}, fmt.Errorf("resolve @%s: %w", publicUsername, err)
	}
	if !resolved.Broadcast {
		return AddResult{}, fmt.Errorf("resolve @%s: peer is not a broadcast channel", publicUsername)
	}

	ch := Channel{
		TelegramID:     resolved.ID,
		PublicUsername: publicUsername,
		Status:         StatusActive,
	}
	var result AddResult

	if resolved.Left {
		if err := s.gateway.JoinChannel(ctx, resolved); err != nil {
			ch.Status = StatusAccessError
			ch.LastError = err.Error()
			added, addErr := s.repo.Add(ctx, ch)
			result.Channel = added
			return result, errors.Join(fmt.Errorf("join @%s: %w", publicUsername, err), addErr)
		}
	}

	discussion, err := s.gateway.ResolveDiscussion(ctx, resolved)
	if err != nil {
		if errors.Is(err, ErrDiscussionUnavailable) {
			ch.Status = StatusDiscussionUnavailable
			ch.LastError = err.Error()
			added, addErr := s.repo.Add(ctx, ch)
			result.Channel = added
			return result, addErr
		}
		return AddResult{}, fmt.Errorf("resolve discussion for @%s: %w", publicUsername, err)
	}
	result.DiscussionFound = true
	if !discussion.CanSend {
		ch.Status = StatusDeliveryUnavailable
		ch.LastError = "the account cannot send messages to the discussion group"
	}
	result.CommentDeliveryReady = ch.Status == StatusActive

	added, err := s.repo.Add(ctx, ch)
	if err != nil {
		return AddResult{}, fmt.Errorf("add @%s: %w", publicUsername, err)
	}
	result.Channel = added
	return result, nil
}

func (s *Service) Delete(ctx context.Context, publicUsername string) error {
	return s.repo.Delete(ctx, publicUsername)
}

func (s *Service) List(ctx context.Context) ([]Channel, error) {
	return s.repo.List(ctx)
}

package channel

import (
	"context"
	"errors"
)

var (
	ErrNotFound              = errors.New("telegram channel not found")
	ErrDiscussionUnavailable = errors.New("telegram channel discussion unavailable")
	ErrDeliveryUnavailable   = errors.New("telegram channel delivery unavailable")
)

type Status string

const (
	StatusActive                Status = "ACTIVE"
	StatusAccessError           Status = "ACCESS_ERROR"
	StatusDiscussionUnavailable Status = "DISCUSSION_UNAVAILABLE"
	StatusDeliveryUnavailable   Status = "DELIVERY_UNAVAILABLE"
)

type Channel struct {
	TelegramID     int64
	PublicUsername string
	Status         Status
	LastError      string
}

// Repository avoids a cyclical postgres package import
type Repository interface {
	Add(context.Context, Channel) (Channel, error)
	Delete(context.Context, string) error
	List(context.Context) ([]Channel, error)
	ChannelByTelegramID(context.Context, int64) (Channel, error)
}

type PeerKind uint8

const (
	PeerUnknown PeerKind = iota
	PeerChat
	PeerChannel
)

// Peer contains the minimum data needed to address a Telegram channel or chat.
// AccessHash is transient and must not be treated as an authentication secret.
type Peer struct {
	Kind       PeerKind
	ID         int64
	AccessHash int64
}

type Resolved struct {
	Peer
	Broadcast bool
	Left      bool
}

type Discussion struct {
	Peer
	CanSend bool
}

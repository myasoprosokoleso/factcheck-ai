package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/myasoprosokoleso/factcheck-ai/internal/channel"
	"github.com/myasoprosokoleso/factcheck-ai/internal/post"
)

type FloodWaitError struct {
	Wait  time.Duration
	cause error
}

func (e *FloodWaitError) Error() string {
	return fmt.Sprintf("telegram: flood wait %s: %v", e.Wait, e.cause)
}

func (e *FloodWaitError) Unwrap() error {
	return e.cause
}

type PublishCommentRequest struct {
	PostID            string
	Channel           channel.Resolved
	Discussion        channel.Discussion
	TelegramMessageID int64
	Text              string
}

func (c *Client) ResolveChannel(ctx context.Context, publicUsername string) (channel.Resolved, error) {
	callCtx, cancel := c.requestContext(ctx)
	defer cancel()

	resolved, err := c.rpc.ContactsResolveUsername(callCtx, &tg.ContactsResolveUsernameRequest{Username: publicUsername})
	if err != nil {
		return channel.Resolved{}, adaptGotdError(err)
	}
	peer, ok := resolved.Peer.(*tg.PeerChannel)
	if !ok {
		return channel.Resolved{}, fmt.Errorf("%w: @%s is not a channel", channel.ErrNotFound, publicUsername)
	}
	for _, candidate := range resolved.Chats {
		resolvedChannel, ok := candidate.(*tg.Channel)
		if !ok || resolvedChannel.ID != peer.ChannelID {
			continue
		}
		return channel.Resolved{
			Peer: channel.Peer{
				Kind:       channel.PeerChannel,
				ID:         resolvedChannel.ID,
				AccessHash: resolvedChannel.AccessHash,
			},
			Broadcast: resolvedChannel.Broadcast,
			Left:      resolvedChannel.Left,
		}, nil
	}
	return channel.Resolved{}, fmt.Errorf("%w: resolved channel @%s has no full peer", channel.ErrNotFound, publicUsername)
}

func (c *Client) JoinChannel(ctx context.Context, resolved channel.Resolved) error {
	callCtx, cancel := c.requestContext(ctx)
	defer cancel()

	_, err := c.rpc.ChannelsJoinChannel(callCtx, inputChannel(resolved.Peer))
	if tgerr.Is(err, "USER_ALREADY_PARTICIPANT") {
		return nil
	}
	return adaptGotdError(err)
}

func (c *Client) ResolveDiscussion(ctx context.Context, resolved channel.Resolved) (channel.Discussion, error) {
	callCtx, cancel := c.requestContext(ctx)
	defer cancel()

	full, err := c.rpc.ChannelsGetFullChannel(callCtx, inputChannel(resolved.Peer))
	if err != nil {
		return channel.Discussion{}, adaptGotdError(err)
	}
	channelFull, ok := full.FullChat.(*tg.ChannelFull)
	if !ok {
		return channel.Discussion{}, channel.ErrDiscussionUnavailable
	}
	linkedID, ok := channelFull.GetLinkedChatID()
	if !ok || linkedID == 0 {
		return channel.Discussion{}, channel.ErrDiscussionUnavailable
	}

	for _, candidate := range full.Chats {
		switch chat := candidate.(type) {
		case *tg.Channel:
			if chat.ID != linkedID {
				continue
			}
			return channel.Discussion{
				Peer: channel.Peer{
					Kind:       channel.PeerChannel,
					ID:         chat.ID,
					AccessHash: chat.AccessHash,
				},
				CanSend: canSendToChannel(chat),
			}, nil
		case *tg.Chat:
			if chat.ID != linkedID {
				continue
			}
			canSend := !chat.Deactivated && !chat.Left
			if rights, ok := chat.GetDefaultBannedRights(); ok && (rights.ViewMessages || rights.SendMessages) {
				canSend = false
			}
			return channel.Discussion{
				Peer: channel.Peer{
					Kind: channel.PeerChat,
					ID:   chat.ID,
				},
				CanSend: canSend,
			}, nil
		}
	}
	return channel.Discussion{}, channel.ErrDiscussionUnavailable
}

func (c *Client) PublishComment(ctx context.Context, request PublishCommentRequest) error {
	if !request.Discussion.CanSend {
		return channel.ErrDeliveryUnavailable
	}
	threadID, err := c.resolveDiscussionMessage(
		ctx, request.Channel, request.Discussion, request.TelegramMessageID,
	)
	if err != nil {
		return err
	}
	return c.sendReply(
		ctx,
		inputPeer(request.Discussion.Peer),
		threadID,
		request.Text,
		StableRandomID(request.PostID),
	)
}

func (c *Client) resolveDiscussionMessage(
	ctx context.Context,
	resolved channel.Resolved,
	discussion channel.Discussion,
	channelMessageID int64,
) (int64, error) {
	callCtx, cancel := c.requestContext(ctx)
	defer cancel()

	result, err := c.rpc.MessagesGetDiscussionMessage(callCtx, &tg.MessagesGetDiscussionMessageRequest{
		Peer:  inputPeer(resolved.Peer),
		MsgID: int(channelMessageID),
	})
	if err != nil {
		return 0, adaptGotdError(err)
	}
	threadID := discussionMessageID(result.Messages, discussion)
	if threadID == 0 {
		return 0, channel.ErrDiscussionUnavailable
	}
	return threadID, nil
}

func (c *Client) sendPrivateMessage(
	ctx context.Context,
	recipientID int64,
	accessHash int64,
	replyToMessageID int64,
	text string,
) error {
	if replyToMessageID <= 0 {
		return errors.New("telegram: reply message ID must be positive")
	}
	textHash := sha256.Sum256([]byte(post.NormalizeText(text)))
	randomID := StableRandomID(fmt.Sprintf("owner:%d:%d:%x", recipientID, replyToMessageID, textHash))
	peer := &tg.InputPeerUser{UserID: recipientID, AccessHash: accessHash}
	return c.sendReply(ctx, peer, replyToMessageID, text, randomID)
}

func (c *Client) sendReply(
	ctx context.Context,
	peer tg.InputPeerClass,
	replyToMessageID int64,
	text string,
	randomID int64,
) error {
	callCtx, cancel := c.requestContext(ctx)
	defer cancel()

	_, err := c.rpc.MessagesSendMessage(callCtx, &tg.MessagesSendMessageRequest{
		Peer:     peer,
		ReplyTo:  &tg.InputReplyToMessage{ReplyToMsgID: int(replyToMessageID)},
		Message:  text,
		RandomID: randomID,
	})
	return adaptGotdError(err)
}

func (c *Client) requestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.timeout)
}

func inputChannel(peer channel.Peer) *tg.InputChannel {
	return &tg.InputChannel{ChannelID: peer.ID, AccessHash: peer.AccessHash}
}

func inputPeer(peer channel.Peer) tg.InputPeerClass {
	switch peer.Kind {
	case channel.PeerChat:
		return &tg.InputPeerChat{ChatID: peer.ID}
	case channel.PeerChannel:
		return &tg.InputPeerChannel{ChannelID: peer.ID, AccessHash: peer.AccessHash}
	default:
		return &tg.InputPeerEmpty{}
	}
}

func canSendToChannel(channel *tg.Channel) bool {
	if channel == nil {
		return false
	}
	if rights, ok := channel.GetBannedRights(); ok && (rights.ViewMessages || rights.SendMessages) {
		return false
	}
	if rights, ok := channel.GetDefaultBannedRights(); ok && (rights.ViewMessages || rights.SendMessages) {
		return false
	}
	return true
}

func gotdPeerID(peer tg.PeerClass) (channel.PeerKind, int64) {
	switch peer := peer.(type) {
	case *tg.PeerChat:
		return channel.PeerChat, peer.ChatID
	case *tg.PeerChannel:
		return channel.PeerChannel, peer.ChannelID
	default:
		return channel.PeerUnknown, 0
	}
}

func discussionMessageID(messages []tg.MessageClass, discussion channel.Discussion) int64 {
	for _, candidate := range messages {
		message, ok := candidate.(*tg.Message)
		if !ok {
			continue
		}
		kind, id := gotdPeerID(message.PeerID)
		if kind == discussion.Kind && id == discussion.ID && message.ID > 0 {
			return int64(message.ID)
		}
	}
	return 0
}

func StableRandomID(postID string) int64 {
	sum := sha256.Sum256([]byte(postID))
	id := int64(binary.BigEndian.Uint64(sum[:8]))
	if id == 0 {
		return 1
	}
	return id
}

func adaptGotdError(err error) error {
	if err == nil {
		return nil
	}
	if wait, ok := tgerr.AsFloodWait(err); ok {
		return &FloodWaitError{Wait: wait, cause: err}
	}
	if tgerr.Is(err,
		"CHAT_WRITE_FORBIDDEN",
		"CHAT_RESTRICTED",
		"USER_BANNED_IN_CHANNEL",
		"CHANNEL_PRIVATE",
		"INPUT_USER_DEACTIVATED",
	) {
		return fmt.Errorf("%w: %v", channel.ErrDeliveryUnavailable, err)
	}
	return err
}

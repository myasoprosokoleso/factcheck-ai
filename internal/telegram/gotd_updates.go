package telegram

import (
	"context"
	"time"

	"github.com/gotd/td/tg"
)

func newGotdDispatcher(handler updateHandler) tg.UpdateDispatcher {
	dispatcher := tg.NewUpdateDispatcher()
	dispatcher.OnNewChannelMessage(func(ctx context.Context, _ tg.Entities, update *tg.UpdateNewChannelMessage) error {
		return dispatchGotdChannelMessage(ctx, handler, update.Message)
	})
	dispatcher.OnNewMessage(func(ctx context.Context, entities tg.Entities, update *tg.UpdateNewMessage) error {
		message, ok := update.Message.(*tg.Message)
		if !ok || message.Out || message.Post {
			return nil
		}
		if _, ok := message.PeerID.(*tg.PeerUser); !ok {
			return nil
		}
		senderPeer, ok := message.FromID.(*tg.PeerUser)
		if !ok || senderPeer.UserID == 0 {
			return nil
		}
		var accessHash int64
		if sender, found := entities.Users[senderPeer.UserID]; found && sender != nil {
			accessHash = sender.AccessHash
		}
		return handler.HandlePrivateMessage(ctx, PrivateMessage{
			MessageID:  int64(message.ID),
			SenderID:   senderPeer.UserID,
			AccessHash: accessHash,
			Text:       message.Message,
		})
	})
	return dispatcher
}

func dispatchGotdChannelMessage(
	ctx context.Context,
	handler updateHandler,
	messageClass tg.MessageClass,
) error {
	message, ok := messageClass.(*tg.Message)
	if !ok || !message.Post || message.Out {
		return nil
	}
	peer, ok := message.PeerID.(*tg.PeerChannel)
	if !ok || peer.ChannelID == 0 {
		return nil
	}
	return handler.HandlePost(ctx, PostUpdate{
		ChannelID:   peer.ChannelID,
		MessageID:   int64(message.ID),
		Text:        message.Message,
		PublishedAt: time.Unix(int64(message.Date), 0).UTC(),
	})
}

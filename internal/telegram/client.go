package telegram

import (
	"context"
	"fmt"
	"sync"
	"time"

	tdtelegram "github.com/gotd/td/telegram"
	gotdupdates "github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
)

type ClientConfig struct {
	APIID          int
	APIHash        string
	SessionPath    string
	RequestTimeout time.Duration
}

type Client struct {
	raw       *tdtelegram.Client
	rpc       *tg.Client
	updates   *gotdupdates.Manager
	timeout   time.Duration
	ready     chan struct{}
	readyOnce sync.Once
}

func NewClient(cfg ClientConfig, handler updateHandler) (*Client, error) {
	session, err := newSessionFileStorage(cfg.SessionPath)
	if err != nil {
		return nil, err
	}
	dispatcher := newGotdDispatcher(handler)

	updateManager := gotdupdates.New(gotdupdates.Config{
		Handler: dispatcher,
	})
	raw := tdtelegram.NewClient(cfg.APIID, cfg.APIHash, tdtelegram.Options{
		SessionStorage: session,
		UpdateHandler:  updateManager,
	})
	return &Client{
		raw:     raw,
		rpc:     raw.API(),
		updates: updateManager,
		timeout: cfg.RequestTimeout,
		ready:   make(chan struct{}),
	}, nil
}

func (c *Client) Ready() <-chan struct{} {
	return c.ready
}

func (c *Client) Run(ctx context.Context) error {
	return c.raw.Run(ctx, func(runCtx context.Context) error {
		status, err := c.raw.Auth().Status(runCtx)
		if err != nil {
			return fmt.Errorf("check telegram authorization: %w", err)
		}
		if !status.Authorized || status.User == nil {
			return errSessionUnauthorized
		}
		c.readyOnce.Do(func() { close(c.ready) })
		return c.updates.Run(runCtx, c.raw.API(), status.User.ID, gotdupdates.AuthOptions{
			IsBot: status.User.Bot,
		})
	})
}

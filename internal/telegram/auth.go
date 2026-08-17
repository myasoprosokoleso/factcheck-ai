package telegram

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	tdtelegram "github.com/gotd/td/telegram"
	tdauth "github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"golang.org/x/term"

	"github.com/myasoprosokoleso/factcheck-ai/internal/config"
)

func InteractiveLogin(ctx context.Context, cfg config.TelegramConfig) error {
	session, err := newSessionFileStorage(cfg.SessionPath)
	if err != nil {
		return err
	}

	authenticator := &userAuthenticator{
		phone: cfg.Phone,
		input: bufio.NewReader(os.Stdin),
	}
	flow := tdauth.NewFlow(authenticator, tdauth.SendCodeOptions{})
	client := tdtelegram.NewClient(
		cfg.APIID,
		cfg.APIHash,
		tdtelegram.Options{SessionStorage: session},
	)

	if err := client.Run(ctx, func(runCtx context.Context) error {
		if err := client.Auth().IfNecessary(runCtx, flow); err != nil {
			return fmt.Errorf("telegram login: %w", err)
		}
		status, err := client.Auth().Status(runCtx)
		if err != nil {
			return fmt.Errorf("verify telegram login: %w", err)
		}
		if !status.Authorized {
			return errSessionUnauthorized
		}
		return nil
	}); err != nil {
		return err
	}

	if err := ensurePrivateFile(session.Path); err != nil {
		return fmt.Errorf("secure telegram session: %w", err)
	}
	return nil
}

// userAuthenticator implements tdauth.UserAuthenticator interface
type userAuthenticator struct {
	phone string
	input *bufio.Reader
}

func (a *userAuthenticator) Phone(context.Context) (string, error) {
	return a.phone, nil
}

func (a *userAuthenticator) Password(context.Context) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return a.read("2FA password: ")
	}

	fmt.Print("2FA password: ")
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}

	password := strings.TrimSpace(string(value))
	if password == "" {
		return "", errors.New("telegram: empty 2FA password")
	}
	return password, nil
}

func (a *userAuthenticator) Code(context.Context, *tg.AuthSentCode) (string, error) {
	return a.read("Telegram code: ")
}

func (*userAuthenticator) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return errors.New("telegram: account sign-up is not supported; create the account before login")
}

func (*userAuthenticator) SignUp(context.Context) (tdauth.UserInfo, error) {
	return tdauth.UserInfo{}, errors.New("telegram: account sign-up is not supported")
}

func (a *userAuthenticator) read(inputMessage string) (string, error) {
	fmt.Print(inputMessage)
	value, err := a.input.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("telegram: empty input value")
	}
	return value, nil
}

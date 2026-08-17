package telegram

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/myasoprosokoleso/factcheck-ai/internal/channel"
)

const (
	commandAdd    = "add"
	commandDelete = "delete"
	commandList   = "list"
)

type command struct {
	name           string
	publicUsername string
}

func parseCommand(input string) (command, error) {
	fields := strings.Fields(input)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return command{}, errors.New("telegram: invalid command")
	}

	name := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	if before, _, found := strings.Cut(name, "@"); found {
		name = before
	}

	cmd := command{name: name}
	switch cmd.name {
	case commandList:
		if len(fields) != 1 {
			return command{}, errors.New("telegram: invalid command: use /list")
		}
		return cmd, nil
	case commandAdd, commandDelete:
		if len(fields) != 2 {
			return command{}, fmt.Errorf("telegram: invalid command: use /%s @channel", cmd.name)
		}
		publicUsername, err := normalizePublicUsername(fields[1])
		if err != nil {
			return command{}, fmt.Errorf("telegram: invalid command: %v", err)
		}
		cmd.publicUsername = publicUsername
		return cmd, nil
	default:
		return command{}, fmt.Errorf("telegram: invalid command: unknown command /%s", name)
	}
}

func normalizePublicUsername(input string) (string, error) {
	username := strings.TrimPrefix(input, "@")
	if len(username) < 4 || len(username) > 32 {
		return "", errors.New("username must contain between 4 and 32 characters")
	}
	for i, r := range username {
		if i == 0 && !unicode.IsLetter(r) {
			return "", errors.New("username must start with a letter")
		}
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_') {
			return "", errors.New("username contains invalid characters")
		}
	}
	return strings.ToLower(username), nil
}

type CommandHandler struct {
	Channels *channel.Service
}

func (h *CommandHandler) handle(ctx context.Context, msg string) (string, error) {
	cmd, err := parseCommand(msg)
	if err != nil {
		return err.Error(), nil
	}
	switch cmd.name {
	case commandAdd:
		result, err := h.Channels.Add(ctx, cmd.publicUsername)
		if err != nil {
			return "", err
		}
		return formatAddResult(result), nil
	case commandDelete:
		if err := h.Channels.Delete(ctx, cmd.publicUsername); err != nil {
			return "", err
		}
		return fmt.Sprintf("Channel @%s was deleted from monitoring.", cmd.publicUsername), nil
	default: // parseCommand permits only /list here
		channels, err := h.Channels.List(ctx)
		if err != nil {
			return "", err
		}
		return formatChannelList(channels), nil
	}
}

func formatAddResult(result channel.AddResult) string {
	discussion := "not found"
	if result.DiscussionFound {
		discussion = "found"
	}
	delivery := "unavailable"
	if result.CommentDeliveryReady {
		delivery = "available"
	}
	monitoring := "disabled"
	if result.Channel.Status == channel.StatusActive {
		monitoring = "enabled"
	}

	publicUsername := result.Channel.PublicUsername
	return fmt.Sprintf(
		"Channel @%s was added.\n\nDiscussion group: %s\nComment delivery: %s\nMonitoring: %s",
		publicUsername,
		discussion,
		delivery,
		monitoring,
	)
}

func formatChannelList(channels []channel.Channel) string {
	if len(channels) == 0 {
		return "The channel list is empty."
	}

	slices.SortFunc(channels, func(a, b channel.Channel) int {
		return strings.Compare(a.PublicUsername, b.PublicUsername)
	})
	lines := make([]string, 0, len(channels))

	for _, channel := range channels {
		lines = append(lines, fmt.Sprintf("@%s — %s", channel.PublicUsername, channel.Status))
	}
	return strings.Join(lines, "\n")
}

package factcheck

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"unicode/utf8"
)

var ErrNotFound = errors.New("fact-check not found")

type Outcome string

const (
	OutcomeSupported            Outcome = "SUPPORTED"
	OutcomeMixed                Outcome = "MIXED"
	OutcomeUnsupported          Outcome = "UNSUPPORTED"
	OutcomeInsufficientEvidence Outcome = "INSUFFICIENT_EVIDENCE"
	OutcomeNotCheckable         Outcome = "NOT_CHECKABLE"
)

func (outcome Outcome) valid() bool {
	switch outcome {
	case OutcomeSupported, OutcomeMixed, OutcomeUnsupported,
		OutcomeInsufficientEvidence, OutcomeNotCheckable:
		return true
	default:
		return false
	}
}

type Source struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

type Result struct {
	Outcome Outcome  `json:"outcome"`
	Sources []Source `json:"sources"`
	Summary string   `json:"summary"`
	Comment string   `json:"comment,omitempty"`
}

type DeliveryWork struct {
	PostID            string
	TelegramChannelID int64
	PublicUsername    string
	TelegramMessageID int64
	CommentText       string
}

// Repository avoids a cyclical postgres package import
type Repository interface {
	SaveResult(context.Context, string, Result) error
	ForDelivery(context.Context, string) (DeliveryWork, error)
}

func (result Result) ShouldComment() bool {
	return result.Outcome == OutcomeUnsupported || result.Outcome == OutcomeMixed
}

func (result Result) valid() bool {
	summary := result.Summary
	if !result.Outcome.valid() || summary == "" || utf8.RuneCountInString(summary) > 1500 {
		return false
	}

	switch result.Outcome {
	case OutcomeSupported, OutcomeMixed, OutcomeUnsupported:
		if len(result.Sources) == 0 {
			return false
		}
	}
	return true
}

func safeSourceURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("source URL is malformed")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("source URL must use HTTP or HTTPS")
	}
	if parsed.User != nil {
		return nil, errors.New("source URL must not contain credentials")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, errors.New("localhost source is forbidden")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
		return nil, errors.New("private source address is forbidden")
	}
	parsed.Fragment = ""
	return parsed, nil
}

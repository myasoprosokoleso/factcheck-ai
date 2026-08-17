package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/myasoprosokoleso/factcheck-ai/internal/openai"
)

const (
	defaultHTTPAddr    = ":8080"
	defaultOpenAIModel = "gpt-5.6-terra"
)

type Config struct {
	LogLevel string

	PostgresDSN string
	HTTPAddr    string

	Telegram TelegramConfig
	OpenAI   openai.Config
}

type TelegramConfig struct {
	APIID       int
	APIHash     string
	Phone       string
	SessionPath string
	OwnerUserID int64
}

func Load() (Config, error) {
	apiID, err := optionalInt("TELEGRAM_API_ID")
	if err != nil {
		return Config{}, err
	}
	ownerID, err := optionalInt64("TELEGRAM_OWNER_USER_ID")
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		LogLevel:    valueOrDefault("LOG_LEVEL", "info"),
		PostgresDSN: strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		HTTPAddr:    valueOrDefault("HTTP_LISTEN_ADDR", defaultHTTPAddr),
		Telegram: TelegramConfig{
			APIID:       apiID,
			APIHash:     strings.TrimSpace(os.Getenv("TELEGRAM_API_HASH")),
			Phone:       strings.TrimSpace(os.Getenv("TELEGRAM_PHONE")),
			SessionPath: strings.TrimSpace(os.Getenv("TELEGRAM_SESSION_PATH")),
			OwnerUserID: ownerID,
		},
		OpenAI: openai.Config{
			APIKey: strings.TrimSpace(os.Getenv("OPENAI_API_KEY")),
			Model:  valueOrDefault("OPENAI_MODEL", defaultOpenAIModel),
		},
	}
	return cfg, nil
}

func (c Config) ValidateServe() error {
	return missing(map[string]bool{
		"POSTGRES_DSN":           c.PostgresDSN != "",
		"OPENAI_API_KEY":         c.OpenAI.APIKey != "",
		"TELEGRAM_API_ID":        c.Telegram.APIID > 0,
		"TELEGRAM_API_HASH":      c.Telegram.APIHash != "",
		"TELEGRAM_SESSION_PATH":  c.Telegram.SessionPath != "",
		"TELEGRAM_OWNER_USER_ID": c.Telegram.OwnerUserID > 0,
	})
}

func (c Config) ValidateTelegramLogin() error {
	return missing(map[string]bool{
		"TELEGRAM_API_ID":       c.Telegram.APIID > 0,
		"TELEGRAM_API_HASH":     c.Telegram.APIHash != "",
		"TELEGRAM_PHONE":        c.Telegram.Phone != "",
		"TELEGRAM_SESSION_PATH": c.Telegram.SessionPath != "",
	})
}

func (c Config) ValidateMigrations() error {
	return missing(map[string]bool{"POSTGRES_DSN": c.PostgresDSN != ""})
}

func (c Config) LogValues() map[string]any {
	return map[string]any{
		"log_level":        c.LogLevel,
		"http_listen_addr": c.HTTPAddr,
		"openai_model":     c.OpenAI.Model,
		"telegram_api_id":  c.Telegram.APIID,
	}
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func optionalInt(name string) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return n, nil
}

func optionalInt64(name string) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return n, nil
}

func missing(values map[string]bool) error {
	var names []string
	for name, ok := range values {
		if !ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}

	slices.Sort(names) // deterministic output
	return errors.New("missing required environment variables: " + strings.Join(names, ", "))
}

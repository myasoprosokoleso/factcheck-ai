package factcheck

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/myasoprosokoleso/factcheck-ai/internal/openai"
)

//go:embed prompts/system_prompt.txt
var systemPrompt string

var errInvalidLLMResponse = errors.New("invalid OpenAI structured response")

type responseWire struct {
	Outcome Outcome `json:"outcome"`
	Summary string  `json:"summary"`
}

type Service struct {
	client *openai.Client
}

func NewService(client *openai.Client) *Service {
	return &Service{client: client}
}

func (service *Service) Check(ctx context.Context, input string) (Result, error) {
	payload, err := json.Marshal(struct {
		Post string `json:"post"`
	}{Post: truncate(input, 30_000)})
	if err != nil {
		return Result{}, fmt.Errorf("encode OpenAI input: %w", err)
	}

	response, err := service.client.Complete(ctx, openai.Request{
		Instructions: systemPrompt,
		Input:        string(payload),
		SchemaName:   "factcheck_result",
		Schema:       resultSchema(),
	})
	if err != nil {
		return Result{}, fmt.Errorf("OpenAI fact-check: %w", err)
	}

	result, err := parseResult(response.Text, response.Citations)
	if err != nil {
		return safeFallback(), nil
	}
	if result.ShouldComment() {
		result.Comment = formatComment(result)
	}
	if !result.valid() {
		return safeFallback(), nil
	}
	return result, nil
}

func safeFallback() Result {
	return Result{
		Outcome: OutcomeInsufficientEvidence,
		Sources: []Source{},
		Summary: "Недостаточно надёжных данных для вывода о достоверности публикации.",
	}
}

func resultSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"outcome", "summary"},
		"properties": map[string]any{
			"outcome": map[string]any{"type": "string", "enum": []string{"SUPPORTED", "MIXED", "UNSUPPORTED", "INSUFFICIENT_EVIDENCE", "NOT_CHECKABLE"}},
			"summary": map[string]any{"type": "string", "minLength": 1},
		},
	}
}

func parseResult(raw string, citations []openai.Citation) (Result, error) {
	var wire responseWire
	if err := decodeStrict(raw, &wire); err != nil {
		return Result{}, fmt.Errorf("%w: %v", errInvalidLLMResponse, err)
	}
	wire.Summary = strings.TrimSpace(wire.Summary)
	sources := citationSources(citations)
	return Result{
		Outcome: wire.Outcome,
		Sources: sources,
		Summary: wire.Summary,
	}, nil
}

func decodeStrict(raw string, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func citationSources(citations []openai.Citation) []Source {
	const maximumCitations = 5

	sources := make([]Source, 0, min(len(citations), maximumCitations))
	seen := make(map[string]struct{}, len(citations))
	for _, citation := range citations {
		parsed, err := safeSourceURL(citation.URL)
		if err != nil {
			continue
		}
		query := parsed.Query()
		for key := range query {
			lower := strings.ToLower(key)
			if strings.HasPrefix(lower, "utm_") || lower == "fbclid" || lower == "gclid" {
				query.Del(key)
			}
		}
		parsed.RawQuery = query.Encode()
		cleanURL := parsed.String()
		if _, duplicate := seen[cleanURL]; duplicate {
			continue
		}
		seen[cleanURL] = struct{}{}
		title := truncate(sanitizeGenerated(citation.Title), 200)
		if title == "" {
			title = parsed.Hostname()
		}
		sources = append(sources, Source{URL: cleanURL, Title: title})
		if len(sources) == maximumCitations {
			break
		}
	}
	return sources
}

func truncate(value string, maximum int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maximum {
		return string(runes)
	}
	return string(runes[:maximum-1]) + "…"
}

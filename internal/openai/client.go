package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultOpenAIBaseURL  = "https://api.openai.com/v1"
	defaultRequestTimeout = 60 * time.Second
	maxResponseBytes      = 2 << 20
	maxErrorBodyBytes     = 4096
)

type Config struct {
	BaseURL    string
	APIKey     string
	Model      string
	TestClient *http.Client
}

type Client struct {
	endpoint   string
	apiKey     string
	model      string
	httpClient *http.Client
}

func New(config Config) (*Client, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid OpenAI base URL %q", baseURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported OpenAI URL scheme %q", parsed.Scheme)
	}
	endpoint := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(endpoint, "/responses") {
		endpoint += "/responses"
	}

	httpClient := config.TestClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}

	return &Client{
		endpoint:   endpoint,
		apiKey:     strings.TrimSpace(config.APIKey),
		model:      strings.TrimSpace(config.Model),
		httpClient: httpClient,
	}, nil
}

func (client *Client) Complete(ctx context.Context, request Request) (Response, error) {
	payload := responsesRequest{
		Model:        client.model,
		Instructions: request.Instructions,
		Input:        request.Input,
		Reasoning:    reasoning{Effort: "low"},
		Text: responseText{
			Verbosity: "low",
			Format: responseTextFormat{
				Type:   "json_schema",
				Name:   request.SchemaName,
				Strict: true,
				Schema: request.Schema,
			},
		},
		Store:           false,
		Tools:           []responseTool{{Type: "web_search"}},
		MaxOutputTokens: 2_500,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("encode OpenAI request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("create OpenAI request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+client.apiKey)

	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return Response{}, fmt.Errorf("perform OpenAI request: %w", err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		errorBody, _ := io.ReadAll(io.LimitReader(httpResponse.Body, maxErrorBodyBytes))
		return Response{}, fmt.Errorf("OpenAI returned HTTP %d: %s", httpResponse.StatusCode, strings.TrimSpace(string(errorBody)))
	}
	responseBody, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxResponseBytes+1))
	if err != nil {
		return Response{}, fmt.Errorf("read OpenAI response: %w", err)
	}
	if len(responseBody) > maxResponseBytes {
		return Response{}, errors.New("OpenAI response exceeds 2 MiB")
	}

	var envelope responsesEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return Response{}, fmt.Errorf("decode OpenAI response: %w", err)
	}
	if envelope.Error != nil {
		return Response{}, fmt.Errorf("OpenAI error: %s", envelope.Error.Message)
	}
	if envelope.Status != "completed" {
		return Response{}, fmt.Errorf("OpenAI response status is %q", envelope.Status)
	}

	var result Response
	for _, item := range envelope.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "refusal" {
				return Response{}, fmt.Errorf("OpenAI refused the request: %s", strings.TrimSpace(content.Refusal))
			}
			if content.Type != "output_text" {
				continue
			}
			result.Text += content.Text
			for _, annotation := range content.Annotations {
				if annotation.Type != "url_citation" {
					continue
				}
				result.Citations = append(result.Citations, Citation{
					URL:   annotation.URL,
					Title: annotation.Title,
				})
			}
		}
	}
	if strings.TrimSpace(result.Text) == "" {
		return Response{}, errors.New("OpenAI response has no output text")
	}
	return result, nil
}

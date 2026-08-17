package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/myasoprosokoleso/factcheck-ai/internal/factcheck"
	"github.com/myasoprosokoleso/factcheck-ai/internal/openai"
)

func TestFactCheckPipelineOverHTTP(t *testing.T) {
	resultResponse := readTextFixture(t, "pipeline", "result.json")
	postText := readTextFixture(t, "pipeline", "post.txt")

	const sourceURL = "https://source.example/report"

	var llmRequests atomic.Int32
	llmServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestNumber := llmRequests.Add(1)
		if request.URL.Path != "/v1/responses" {
			t.Errorf("OpenAI path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("OpenAI authorization = %q", request.Header.Get("Authorization"))
		}

		var payload struct {
			Model string `json:"model"`
			Text  struct {
				Format struct {
					Type   string `json:"type"`
					Name   string `json:"name"`
					Strict bool   `json:"strict"`
				} `json:"format"`
			} `json:"text"`
			Tools []struct {
				Type string `json:"type"`
			} `json:"tools"`
			Store bool `json:"store"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode OpenAI request: %v", err)
		}
		if payload.Model != "demo-model" || payload.Text.Format.Type != "json_schema" || !payload.Text.Format.Strict || payload.Store {
			t.Errorf("unexpected OpenAI request: %+v", payload)
		}

		if payload.Text.Format.Name != "factcheck_result" {
			t.Errorf("unexpected schema %q", payload.Text.Format.Name)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(payload.Tools) != 1 || payload.Tools[0].Type != "web_search" {
			t.Errorf("tools = %+v, want web_search", payload.Tools)
		}
		responseText := resultResponse
		switch requestNumber {
		case 2:
			responseText = `{
					"outcome":"MIXED",
				"summary":"Дооо, братан, показатель вырос вообще везде — конечно-конечно. В целом рост был, но в нескольких регионах зафиксировано снижение. Слово «всех» превращает нормальную статистику в манипулятивную херню."
			}`
		case 3:
			responseText = `{
					"outcome":"SUPPORTED",
				"summary":"Официальный отчёт подтверждает опубликованный показатель."
			}`
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"status": "completed",
			"output": []map[string]any{{
				"type": "message",
				"content": []map[string]any{{
					"type": "output_text", "text": responseText,
					"annotations": []map[string]any{{
						"type": "url_citation", "url": sourceURL, "title": "Official report",
					}},
				}},
			}},
		})
	}))
	defer llmServer.Close()

	httpClient := llmServer.Client()
	httpClient.Timeout = 3 * time.Second
	openAIClient, err := openai.New(openai.Config{
		BaseURL:    llmServer.URL + "/v1",
		APIKey:     "test-key",
		Model:      "demo-model",
		TestClient: httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := factcheck.NewService(openAIClient)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	result, err := service.Check(ctx, postText)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Outcome != factcheck.OutcomeUnsupported {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Sources) != 1 {
		t.Fatalf("incomplete pipeline result: %+v", result)
	}
	item := result.Sources[0]
	if item.Title != "Official report" {
		t.Fatalf("unexpected source: %+v", item)
	}
	if item.URL != sourceURL {
		t.Fatalf("unexpected source URL: %q", item.URL)
	}
	if !strings.Contains(result.Comment, item.URL) {
		t.Fatalf("citation did not reach comment: source=%+v comment=%q", item, result.Comment)
	}
	if !strings.HasPrefix(result.Comment, "FACT CHECK STATUS: FALSE ❌\n\n") {
		t.Fatalf("unsupported comment did not use the expected format: %q", result.Comment)
	}
	if !strings.Contains(result.Comment, "Пруфы, с которыми хрен поспоришь:") {
		t.Fatalf("unsupported comment did not use the expected sources heading: %q", result.Comment)
	}
	mixed, err := service.Check(ctx, "Показатель вырос на 10 процентов во всех регионах.")
	if err != nil {
		t.Fatalf("Check(mixed) error = %v", err)
	}
	if mixed.Outcome != factcheck.OutcomeMixed {
		t.Fatalf("unexpected mixed result: %+v", mixed)
	}
	if !strings.HasPrefix(mixed.Comment, "FACT CHECK STATUS: FALSE ❌\n\n") ||
		!strings.Contains(mixed.Comment, sourceURL) {
		t.Fatalf("mixed fact did not produce the expected comment: %q", mixed.Comment)
	}

	supported, err := service.Check(ctx, "Показатель вырос на 10 процентов.")
	if err != nil {
		t.Fatalf("Check(supported) error = %v", err)
	}
	if supported.Outcome != factcheck.OutcomeSupported {
		t.Fatalf("unexpected supported result: %+v", supported)
	}
	if supported.Comment != "" {
		t.Fatalf("supported fact produced a comment: %q", supported.Comment)
	}
	if llmRequests.Load() != 3 {
		t.Fatalf("OpenAI requests = %d, want 3", llmRequests.Load())
	}
}

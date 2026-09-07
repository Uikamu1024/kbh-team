package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/internal/domain"
)

// OpenAICompatibleLLM calls APIs that implement the OpenAI Chat Completions
// request and response format.
type OpenAICompatibleLLM struct {
	client  *http.Client
	baseURL string
	apiKey  string
	model   string
}

// NewOpenAICompatibleLLM creates an OpenAI-compatible LLM. A nil client uses
// the default HTTP client.
func NewOpenAICompatibleLLM(client *http.Client, baseURL, apiKey, model string) *OpenAICompatibleLLM {
	if client == nil {
		client = http.DefaultClient
	}
	return &OpenAICompatibleLLM{
		client:  client,
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		model:   strings.TrimSpace(model),
	}
}

// ScoreTopic asks the configured Chat Completions endpoint to score a topic.
// Without an API key, a deterministic local mock is used instead.
func (o *OpenAICompatibleLLM) ScoreTopic(ctx context.Context, topic domain.Topic, previousTopics []string) (int, bool, error) {
	if o == nil || strings.TrimSpace(o.apiKey) == "" {
		return mockScoreTopic(topic, previousTopics), IsTopicNew(topic, previousTopics), nil
	}
	return scoreTopicViaPrompt(ctx, o.sendPrompt, "OpenAI-compatible provider", topic, previousTopics)
}

// GenerateScript asks the configured Chat Completions endpoint to generate a
// script for the selected topics, one call for the greeting and one call per
// chapter. Without an API key, a deterministic local mock is used instead.
func (o *OpenAICompatibleLLM) GenerateScript(ctx context.Context, selected []domain.SelectedTopic) (string, []domain.ChapterDraft, error) {
	if o == nil || strings.TrimSpace(o.apiKey) == "" {
		greetingText, chapters := mockGenerateScript(selected)
		return greetingText, chapters, nil
	}
	return generateScriptViaChapters(ctx, o.sendPrompt, "OpenAI-compatible provider", selected)
}

// ExtractMetadata asks the configured Chat Completions endpoint to classify
// tags and extract UI/summary metadata for one article via structured output
// (response_format: json_schema). Without an API key, a conservative local
// mock is used instead (see mockExtractMetadata).
func (o *OpenAICompatibleLLM) ExtractMetadata(ctx context.Context, title, body string) (domain.ArticleMetadata, error) {
	if o == nil || strings.TrimSpace(o.apiKey) == "" {
		return mockExtractMetadata(title), nil
	}
	return extractMetadataViaPrompt(ctx, o.sendStructuredPrompt, "OpenAI-compatible provider", title, body)
}

// DetectDuplicates asks the configured Chat Completions endpoint, in one
// call, to group newTitles among themselves and against existingGroups via
// structured output. Without an API key, every new title gets its own
// freshly minted group (see mockDetectDuplicates).
func (o *OpenAICompatibleLLM) DetectDuplicates(ctx context.Context, newTitles []string, existingGroups []domain.ExistingTopicGroup) ([]domain.DuplicateAssignment, error) {
	if o == nil || strings.TrimSpace(o.apiKey) == "" {
		return mockDetectDuplicates(newTitles)
	}
	return detectDuplicatesViaPrompt(ctx, o.sendStructuredPrompt, "OpenAI-compatible provider", newTitles, existingGroups)
}

// sendPrompt sends one raw prompt as a single-message chat completion and
// returns the raw text response.
func (o *OpenAICompatibleLLM) sendPrompt(ctx context.Context, prompt string) (string, error) {
	requestBody, err := buildOpenAIChatRequest(o.model, prompt)
	if err != nil {
		return "", err
	}
	return o.chatCompletions(ctx, requestBody)
}

// sendStructuredPrompt sends one prompt as a single-message chat completion
// with response_format: json_schema, constraining the response to the given
// (provider-neutral) JSON Schema.
//
// UNVERIFIED against a live call — backend/docs/generation/02-ingestion.md
// step 5 notes that structured-output support (specifically strict
// json_schema, as opposed to the looser response_format: json_object used by
// scoring/script generation) has not been confirmed for Ollama Cloud's
// available models (Gemma-family models are the working assumption) or for
// OpenRouter's free-tier models. If a model rejects json_schema outright,
// this provider needs a fallback to response_format: json_object plus
// stricter prompt-side JSON-shape instructions instead — not implemented
// here since it requires live verification against real models.
func (o *OpenAICompatibleLLM) sendStructuredPrompt(ctx context.Context, prompt string, schema map[string]any) (string, error) {
	requestBody, err := buildOpenAIChatRequestWithSchema(o.model, prompt, schema)
	if err != nil {
		return "", err
	}
	return o.chatCompletions(ctx, requestBody)
}

type openAIChatRequest struct {
	Model          string               `json:"model"`
	Messages       []openAIChatMessage  `json:"messages"`
	ResponseFormat openAIResponseFormat `json:"response_format"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponseFormat struct {
	Type       string            `json:"type"`
	JSONSchema *openAIJSONSchema `json:"json_schema,omitempty"`
}

type openAIJSONSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func buildOpenAIChatRequest(model, prompt string) ([]byte, error) {
	return json.Marshal(openAIChatRequest{
		Model: model,
		Messages: []openAIChatMessage{
			{Role: "user", Content: prompt},
		},
		ResponseFormat: openAIResponseFormat{Type: "json_object"},
	})
}

// buildOpenAIChatRequestWithSchema is like buildOpenAIChatRequest but
// additionally constrains the response to schema (a provider-neutral JSON
// Schema, see metadataResponseSchema/duplicateResponseSchema in common.go)
// via response_format: json_schema with strict mode.
func buildOpenAIChatRequestWithSchema(model, prompt string, schema map[string]any) ([]byte, error) {
	return json.Marshal(openAIChatRequest{
		Model: model,
		Messages: []openAIChatMessage{
			{Role: "user", Content: prompt},
		},
		ResponseFormat: openAIResponseFormat{
			Type: "json_schema",
			JSONSchema: &openAIJSONSchema{
				Name:   "structured_response",
				Strict: true,
				Schema: schema,
			},
		},
	})
}

// maxChatCompletionAttempts bounds retries for transient failures (429/5xx).
// Free-tier models on shared pools (OpenRouter, etc.) return brief 429s under
// load; a script generation run makes one of these calls per fetched
// article, so without a retry a single transient rate limit aborts the
// entire generation even though most calls succeed.
const maxChatCompletionAttempts = 3

func (o *OpenAICompatibleLLM) chatCompletions(ctx context.Context, requestBody []byte) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= maxChatCompletionAttempts; attempt++ {
		content, retryAfter, err := o.chatCompletionsOnce(ctx, requestBody)
		if err == nil {
			return content, nil
		}
		lastErr = err
		if retryAfter <= 0 || attempt == maxChatCompletionAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(retryAfter):
		}
	}
	return "", lastErr
}

// chatCompletionsOnce makes a single attempt. When it returns a non-nil
// error alongside a positive duration, the caller should wait that long and
// retry; a zero duration means the error isn't worth retrying (bad request,
// auth failure, etc.).
func (o *OpenAICompatibleLLM) chatCompletionsOnce(ctx context.Context, requestBody []byte) (string, time.Duration, error) {
	endpoint := o.baseURL + "/chat/completions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return "", 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+o.apiKey)

	response, err := o.client.Do(request)
	if err != nil {
		return "", 0, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", 0, err
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
		wait := retryAfterDuration(response.Header.Get("Retry-After"))
		return "", wait, fmt.Errorf("OpenAI-compatible provider returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", 0, fmt.Errorf("OpenAI-compatible provider returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var result openAIChatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", 0, fmt.Errorf("decode OpenAI-compatible provider response: %w", err)
	}
	if result.Error != nil {
		// OpenRouter (and similar aggregators) can return HTTP 200 with an
		// error object embedded in the body when the routed upstream model
		// itself fails (e.g. "Provider returned error") — this is the same
		// kind of transient failure as a 429/5xx at the transport level, and
		// is common with free-tier models under load, so it gets the same
		// retry treatment. A code in the 4xx range (bad request, auth) is
		// not retried since retrying would fail identically.
		err := fmt.Errorf("OpenAI-compatible provider API error: %s", result.Error.Message)
		if result.Error.Code != 0 && result.Error.Code < http.StatusInternalServerError && result.Error.Code != http.StatusTooManyRequests {
			return "", 0, err
		}
		return "", retryAfterDuration(""), err
	}
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", 0, errors.New("OpenAI-compatible provider response did not contain a choice")
	}
	return result.Choices[0].Message.Content, 0, nil
}

// retryAfterDuration parses a Retry-After header (seconds, per RFC 9110)
// and falls back to a short fixed backoff when absent or unparseable.
func retryAfterDuration(header string) time.Duration {
	const fallback = 2 * time.Second
	if header == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || seconds <= 0 {
		return fallback
	}
	const maxWait = 15 * time.Second
	wait := time.Duration(seconds) * time.Second
	if wait > maxWait {
		return maxWait
	}
	return wait
}

var _ LLM = (*OpenAICompatibleLLM)(nil)

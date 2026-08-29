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
		return mockScoreTopic(topic, previousTopics), mockIsNew(topic, previousTopics), nil
	}

	prompt, err := buildScorePrompt(topic, previousTopics)
	if err != nil {
		return 0, false, err
	}
	requestBody, err := buildOpenAIChatRequest(o.model, string(prompt))
	if err != nil {
		return 0, false, err
	}
	apiResponse, err := o.chatCompletions(ctx, requestBody)
	if err != nil {
		return 0, false, err
	}
	return parseScoreResult(apiResponse, "OpenAI-compatible provider")
}

// GenerateScript asks the configured Chat Completions endpoint to generate a
// script for the selected topics. Without an API key, a deterministic local
// mock is used instead.
func (o *OpenAICompatibleLLM) GenerateScript(ctx context.Context, selected []domain.ScoredTopic) (string, []domain.ChapterDraft, error) {
	if o == nil || strings.TrimSpace(o.apiKey) == "" {
		greetingText, chapters := mockGenerateScript(selected)
		return greetingText, chapters, nil
	}

	prompt, err := buildScriptPrompt(selected)
	if err != nil {
		return "", nil, err
	}
	requestBody, err := buildOpenAIChatRequest(o.model, string(prompt))
	if err != nil {
		return "", nil, err
	}
	apiResponse, err := o.chatCompletions(ctx, requestBody)
	if err != nil {
		return "", nil, err
	}
	return parseScriptResult(apiResponse, selected, "OpenAI-compatible provider")
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
	Type string `json:"type"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
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
		return "", 0, fmt.Errorf("OpenAI-compatible provider API error: %s", result.Error.Message)
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

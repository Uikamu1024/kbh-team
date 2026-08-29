package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

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

func (o *OpenAICompatibleLLM) chatCompletions(ctx context.Context, requestBody []byte) (string, error) {
	endpoint := o.baseURL + "/chat/completions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+o.apiKey)

	response, err := o.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("OpenAI-compatible provider returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var result openAIChatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode OpenAI-compatible provider response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("OpenAI-compatible provider API error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", errors.New("OpenAI-compatible provider response did not contain a choice")
	}
	return result.Choices[0].Message.Content, nil
}

var _ LLM = (*OpenAICompatibleLLM)(nil)

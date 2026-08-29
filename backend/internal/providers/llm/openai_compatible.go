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

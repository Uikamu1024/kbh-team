package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// This file is intentionally self-contained: it does not import
// backend/internal/providers/llm. cmd/gentags is a standalone, throwaway
// developer tool for proposing a new config/tags.json taxonomy, not part of
// the production pipeline, so it keeps its own minimal provider-calling code
// rather than growing the production LLM interface for a one-off need.

const (
	defaultGeminiModel       = "gemini-3.6-flash"
	defaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"
	defaultOpenRouterModel   = "meta-llama/llama-3.1-8b-instruct:free"
	defaultOllamaBaseURL     = "https://ollama.com/v1"
	defaultOllamaModel       = "gpt-oss:20b"

	maxAttempts = 3
)

// tagsResponse is the structured-output shape every provider is constrained
// to: a JSON object with one "tags" array-of-strings field.
type tagsResponse struct {
	Tags []string `json:"tags"`
}

// proposeTags calls the configured provider asking for tags matching prompt,
// constrained to a JSON {"tags": [...]} response via structured output. If
// the returned count falls outside [minCount, maxCount], it retries once
// with a corrective follow-up appended to the prompt.
func proposeTags(ctx context.Context, client *http.Client, prompt string, minCount, maxCount int) ([]string, error) {
	tags, err := callLLMStructured(ctx, client, prompt)
	if err != nil {
		return nil, err
	}
	if len(tags) >= minCount && len(tags) <= maxCount {
		return tags, nil
	}

	corrected := fmt.Sprintf("%s\n\n直前の指示への回答は%d個だったが、必ず%d〜%d個のタグで出し直してください。", prompt, len(tags), minCount, maxCount)
	tags, err = callLLMStructured(ctx, client, corrected)
	if err != nil {
		return nil, err
	}
	return tags, nil
}

// callLLMStructured dispatches to whichever provider LLM_PROVIDER selects
// (same env vars as the production pipeline, for credential reuse only).
func callLLMStructured(ctx context.Context, client *http.Client, prompt string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LLM_PROVIDER"))) {
	case "openrouter":
		model := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
		if model == "" {
			model = defaultOpenRouterModel
		}
		return callOpenAICompatible(ctx, client, defaultOpenRouterBaseURL, os.Getenv("OPENROUTER_API_KEY"), model, prompt)
	case "ollama":
		model := strings.TrimSpace(os.Getenv("OLLAMA_MODEL"))
		if model == "" {
			model = defaultOllamaModel
		}
		return callOpenAICompatible(ctx, client, defaultOllamaBaseURL, os.Getenv("OLLAMA_API_KEY"), model, prompt)
	default:
		return callGemini(ctx, client, prompt)
	}
}

// --- Gemini ---

type geminiRequest struct {
	Contents         []geminiContent `json:"contents"`
	GenerationConfig geminiGenConfig `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConfig struct {
	ResponseMIMEType string         `json:"responseMimeType"`
	ResponseSchema   map[string]any `json:"responseSchema"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func geminiTagsSchema() map[string]any {
	return map[string]any{
		"type": "OBJECT",
		"properties": map[string]any{
			"tags": map[string]any{
				"type":  "ARRAY",
				"items": map[string]any{"type": "STRING"},
			},
		},
		"required": []string{"tags"},
	}
}

func callGemini(ctx context.Context, client *http.Client, prompt string) ([]string, error) {
	apiKey := strings.TrimSpace(os.Getenv("LLM_API_KEY"))
	if apiKey == "" {
		return nil, errors.New("LLM_API_KEY is not set")
	}
	model := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
	if model == "" {
		model = defaultGeminiModel
	}

	requestBody, err := json.Marshal(geminiRequest{
		Contents: []geminiContent{{Parts: []geminiPart{{Text: prompt}}}},
		GenerationConfig: geminiGenConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema:   geminiTagsSchema(),
		},
	})
	if err != nil {
		return nil, err
	}

	endpoint, err := url.Parse(fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", url.PathEscape(model)))
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("key", apiKey)
	endpoint.RawQuery = query.Encode()

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		text, retryable, err := doJSONPost(ctx, client, endpoint.String(), nil, requestBody, func(body []byte) (string, error) {
			var result geminiResponse
			if err := json.Unmarshal(body, &result); err != nil {
				return "", fmt.Errorf("decode Gemini response: %w", err)
			}
			if result.Error != nil {
				return "", fmt.Errorf("Gemini API error: %s", result.Error.Message)
			}
			if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
				return "", errors.New("Gemini response did not contain a candidate")
			}
			return result.Candidates[0].Content.Parts[0].Text, nil
		})
		if err == nil {
			return parseTagsResponse(text)
		}
		lastErr = err
		if !retryable || attempt == maxAttempts {
			break
		}
		time.Sleep(2 * time.Second)
	}
	return nil, lastErr
}

// --- OpenAI-compatible (OpenRouter, Ollama Cloud) ---

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
	Type       string           `json:"type"`
	JSONSchema openAIJSONSchema `json:"json_schema"`
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

func openAITagsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tags": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
		"required":             []string{"tags"},
		"additionalProperties": false,
	}
}

func callOpenAICompatible(ctx context.Context, client *http.Client, baseURL, apiKey, model, prompt string) ([]string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("API key is not set for this provider")
	}

	requestBody, err := json.Marshal(openAIChatRequest{
		Model:    model,
		Messages: []openAIChatMessage{{Role: "user", Content: prompt}},
		ResponseFormat: openAIResponseFormat{
			Type: "json_schema",
			JSONSchema: openAIJSONSchema{
				Name:   "tags",
				Strict: true,
				Schema: openAITagsSchema(),
			},
		},
	})
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
	headers := map[string]string{"Authorization": "Bearer " + apiKey}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		text, retryable, err := doJSONPost(ctx, client, endpoint, headers, requestBody, func(body []byte) (string, error) {
			var result openAIChatResponse
			if err := json.Unmarshal(body, &result); err != nil {
				return "", fmt.Errorf("decode OpenAI-compatible response: %w", err)
			}
			if result.Error != nil {
				return "", fmt.Errorf("OpenAI-compatible API error: %s", result.Error.Message)
			}
			if len(result.Choices) == 0 {
				return "", errors.New("OpenAI-compatible response did not contain a choice")
			}
			return result.Choices[0].Message.Content, nil
		})
		if err == nil {
			return parseTagsResponse(text)
		}
		lastErr = err
		if !retryable || attempt == maxAttempts {
			break
		}
		time.Sleep(2 * time.Second)
	}
	return nil, lastErr
}

// --- shared HTTP + parsing helpers ---

// doJSONPost POSTs requestBody as JSON, retrying 429/5xx (returns
// retryable=true) but not other errors. extract pulls the raw text payload
// out of the (provider-specific) decoded response body.
func doJSONPost(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, requestBody []byte, extract func([]byte) (string, error)) (text string, retryable bool, err error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return "", false, err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := client.Do(request)
	if err != nil {
		return "", false, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", false, err
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
		return "", true, fmt.Errorf("provider returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", false, fmt.Errorf("provider returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	text, err = extract(body)
	if err != nil {
		return "", false, err
	}
	return text, false, nil
}

func parseTagsResponse(text string) ([]string, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var result tagsResponse
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		// Some providers/models don't actually honor response_format /
		// responseSchema and return a plain markdown bullet or numbered
		// list instead of JSON. Fall back to parsing that shape rather
		// than failing the whole chunk.
		if tags, ok := parseBulletList(text); ok {
			result.Tags = tags
		} else if tags, ok := parseCommaList(text); ok {
			result.Tags = tags
		} else {
			return nil, fmt.Errorf("decode tags response: %w (raw: %s)", err, text)
		}
	}

	seen := make(map[string]bool, len(result.Tags))
	tags := make([]string, 0, len(result.Tags))
	for _, tag := range result.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags, nil
}

// bulletLinePattern matches one markdown list item: an optional leading
// "-"/"*"/"•" or "1."/"1)" marker, followed by the tag text. Trailing
// parenthetical/explanatory text after a colon or em-dash is stripped since
// some models add one-line justifications per bullet.
var bulletLinePattern = regexp.MustCompile(`^\s*(?:[-*•]|\d+[.)])\s*(.+)$`)

// parseBulletList falls back to extracting tags from a plain markdown
// bullet/numbered list when a model ignores the requested JSON schema
// entirely. Returns ok=false if no line looks like a list item, so the
// caller can still report the original JSON decode error.
func parseBulletList(text string) (tags []string, ok bool) {
	for _, line := range strings.Split(text, "\n") {
		match := bulletLinePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		tag := strings.TrimSpace(match[1])
		if index := strings.IndexAny(tag, ":："); index >= 0 {
			tag = strings.TrimSpace(tag[:index])
		}
		if index := strings.Index(tag, "—"); index >= 0 {
			tag = strings.TrimSpace(tag[:index])
		}
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags, len(tags) > 0
}

// commaSplitPattern splits on a half-width or full-width comma or
// Japanese/Chinese "、" enumeration comma, optionally surrounded by
// whitespace.
var commaSplitPattern = regexp.MustCompile(`\s*[,、，]\s*`)

// parseCommaList falls back to a single-line, comma-separated response
// (e.g. "AI, ソフトウェア, Web開発, ..."), the other common shape a model
// returns instead of the requested JSON. Requires at least two items and no
// newlines, so it doesn't misfire on a JSON decode error caused by something
// else entirely.
func parseCommaList(text string) (tags []string, ok bool) {
	if strings.Contains(text, "\n") {
		return nil, false
	}
	for _, part := range commaSplitPattern.Split(text, -1) {
		part = strings.TrimSpace(part)
		if part != "" {
			tags = append(tags, part)
		}
	}
	return tags, len(tags) >= 2
}

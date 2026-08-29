package llm

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
	"strings"
	"time"

	"backend/internal/domain"
)

const defaultGeminiModel = "gemini-3.6-flash"

// GeminiLLM calls the Gemini generateContent API for topic scoring.
type GeminiLLM struct {
	client *http.Client
}

// NewGeminiLLM creates a Gemini-backed LLM. A nil client uses the default HTTP
// client.
func NewGeminiLLM(client *http.Client) *GeminiLLM {
	if client == nil {
		client = http.DefaultClient
	}
	return &GeminiLLM{client: client}
}

// ScoreTopic asks Gemini to score a topic and identify whether it is new.
// Without LLM_API_KEY, a small deterministic rule set is used instead.
func (g *GeminiLLM) ScoreTopic(ctx context.Context, topic domain.Topic, previousTopics []string) (score int, isNew bool, err error) {
	if strings.TrimSpace(os.Getenv("LLM_API_KEY")) == "" {
		return mockScoreTopic(topic, previousTopics), IsTopicNew(topic, previousTopics), nil
	}
	return scoreTopicViaPrompt(ctx, g.sendPrompt, "Gemini", topic, previousTopics)
}

// GenerateScript creates a greeting and one conversational chapter per topic,
// one Gemini call for the greeting and one call per chapter. Without
// LLM_API_KEY, it returns a deterministic local mock script.
func (g *GeminiLLM) GenerateScript(ctx context.Context, selected []domain.SelectedTopic) (string, []domain.ChapterDraft, error) {
	if strings.TrimSpace(os.Getenv("LLM_API_KEY")) == "" {
		greetingText, chapters := mockGenerateScript(selected)
		return greetingText, chapters, nil
	}
	return generateScriptViaChapters(ctx, g.sendPrompt, "Gemini", selected)
}

// ExtractMetadata asks Gemini to classify tags and extract UI/summary
// metadata for one article via structured output (responseSchema). Without
// LLM_API_KEY, a conservative local mock is used instead (see
// mockExtractMetadata).
func (g *GeminiLLM) ExtractMetadata(ctx context.Context, title, body string) (domain.ArticleMetadata, error) {
	if strings.TrimSpace(os.Getenv("LLM_API_KEY")) == "" {
		return mockExtractMetadata(title), nil
	}
	return extractMetadataViaPrompt(ctx, g.sendStructuredPrompt, "Gemini", title, body)
}

// DetectDuplicates asks Gemini, in one call, to group newTitles among
// themselves and against existingGroups via structured output. Without
// LLM_API_KEY, every new title gets its own freshly minted group (see
// mockDetectDuplicates).
func (g *GeminiLLM) DetectDuplicates(ctx context.Context, newTitles []string, existingGroups []domain.ExistingTopicGroup) ([]domain.DuplicateAssignment, error) {
	if strings.TrimSpace(os.Getenv("LLM_API_KEY")) == "" {
		return mockDetectDuplicates(newTitles)
	}
	return detectDuplicatesViaPrompt(ctx, g.sendStructuredPrompt, "Gemini", newTitles, existingGroups)
}

// sendPrompt sends one raw prompt as a Gemini generateContent request and
// returns the raw text response.
func (g *GeminiLLM) sendPrompt(ctx context.Context, prompt string) (string, error) {
	requestBody, err := buildGenerateContentRequest(prompt)
	if err != nil {
		return "", err
	}
	return g.generateContent(ctx, requestBody)
}

// sendStructuredPrompt sends one prompt as a Gemini generateContent request
// with generationConfig.responseSchema set, constraining the response to the
// given (provider-neutral) JSON Schema translated into Gemini's dialect.
func (g *GeminiLLM) sendStructuredPrompt(ctx context.Context, prompt string, schema map[string]any) (string, error) {
	requestBody, err := buildGenerateContentRequestWithSchema(prompt, schema)
	if err != nil {
		return "", err
	}
	return g.generateContent(ctx, requestBody)
}

type generateContentRequest struct {
	Contents         []geminiContent  `json:"contents"`
	GenerationConfig generationConfig `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type generationConfig struct {
	ResponseMIMEType string         `json:"responseMimeType"`
	ResponseSchema   map[string]any `json:"responseSchema,omitempty"`
}

type generateContentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func buildGenerateContentRequest(prompt string) ([]byte, error) {
	request := generateContentRequest{
		Contents: []geminiContent{{Parts: []geminiPart{{Text: prompt}}}},
		GenerationConfig: generationConfig{
			ResponseMIMEType: "application/json",
		},
	}
	return json.Marshal(request)
}

// buildGenerateContentRequestWithSchema is like buildGenerateContentRequest
// but additionally constrains the response to schema (a provider-neutral
// JSON Schema, see metadataResponseSchema/duplicateResponseSchema in
// common.go), translated into Gemini's responseSchema dialect.
func buildGenerateContentRequestWithSchema(prompt string, schema map[string]any) ([]byte, error) {
	request := generateContentRequest{
		Contents: []geminiContent{{Parts: []geminiPart{{Text: prompt}}}},
		GenerationConfig: generationConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema:   toGeminiSchema(schema),
		},
	}
	return json.Marshal(request)
}

// toGeminiSchema translates a standard JSON Schema map (as built by
// metadataResponseSchema/duplicateResponseSchema in common.go) into Gemini's
// responseSchema dialect (a subset of OpenAPI 3.0): type names are
// upper-cased (STRING/OBJECT/ARRAY/INTEGER), and a ["x","null"] type union
// becomes {"type":"X","nullable":true} since Gemini has no type-union
// syntax. UNVERIFIED against a live Gemini structured-output call — flagged
// for confirmation per backend/docs/generation/02-ingestion.md step 5's
// "対応プロバイダ...実装時に確認すること".
func toGeminiSchema(schema map[string]any) map[string]any {
	result := make(map[string]any, len(schema))
	for key, value := range schema {
		switch key {
		case "type":
			geminiType, nullable := toGeminiType(value)
			result["type"] = geminiType
			if nullable {
				result["nullable"] = true
			}
		case "properties":
			properties, ok := value.(map[string]any)
			if !ok {
				result[key] = value
				continue
			}
			converted := make(map[string]any, len(properties))
			for propName, propSchema := range properties {
				if propMap, ok := propSchema.(map[string]any); ok {
					converted[propName] = toGeminiSchema(propMap)
				} else {
					converted[propName] = propSchema
				}
			}
			result[key] = converted
		case "items":
			if itemsMap, ok := value.(map[string]any); ok {
				result[key] = toGeminiSchema(itemsMap)
			} else {
				result[key] = value
			}
		case "additionalProperties":
			// Not part of Gemini's responseSchema dialect; drop it.
		default:
			result[key] = value
		}
	}
	return result
}

// toGeminiType converts a JSON Schema "type" value (a single string, or a
// ["x","null"] union for a nullable field) into Gemini's upper-cased type
// name plus whether the field is nullable.
func toGeminiType(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return strings.ToUpper(typed), false
	case []string:
		nullable := false
		primary := ""
		for _, entry := range typed {
			if entry == "null" {
				nullable = true
				continue
			}
			primary = entry
		}
		return strings.ToUpper(primary), nullable
	default:
		return "STRING", false
	}
}

// generateContent retries transient failures (429/5xx) like
// OpenAICompatibleLLM.chatCompletions does, for the same reason: a script
// generation run makes one call per fetched article, so a single transient
// rate limit shouldn't abort the whole batch.
func (g *GeminiLLM) generateContent(ctx context.Context, requestBody []byte) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= maxChatCompletionAttempts; attempt++ {
		content, retryAfter, err := g.generateContentOnce(ctx, requestBody)
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

func (g *GeminiLLM) generateContentOnce(ctx context.Context, requestBody []byte) (string, time.Duration, error) {
	apiKey := strings.TrimSpace(os.Getenv("LLM_API_KEY"))
	model := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
	if model == "" {
		model = defaultGeminiModel
	}
	endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", url.PathEscape(model))
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil {
		return "", 0, err
	}
	query := parsedEndpoint.Query()
	query.Set("key", apiKey)
	parsedEndpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedEndpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return "", 0, err
	}
	request.Header.Set("Content-Type", "application/json")

	client := g.client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
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
		return "", wait, fmt.Errorf("Gemini returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", 0, fmt.Errorf("Gemini returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var result generateContentResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", 0, fmt.Errorf("decode Gemini response: %w", err)
	}
	if result.Error != nil {
		return "", 0, fmt.Errorf("Gemini API error: %s", result.Error.Message)
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", 0, errors.New("Gemini response did not contain a candidate")
	}
	return result.Candidates[0].Content.Parts[0].Text, 0, nil
}

var _ LLM = (*GeminiLLM)(nil)

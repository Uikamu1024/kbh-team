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
		return mockScoreTopic(topic, previousTopics), mockIsNew(topic, previousTopics), nil
	}

	requestBody, err := buildScoreRequest(topic, previousTopics)
	if err != nil {
		return 0, false, err
	}

	apiResponse, err := g.generateContent(ctx, requestBody)
	if err != nil {
		return 0, false, err
	}

	return parseScoreResult(apiResponse, "Gemini")
}

// GenerateScript creates a greeting and one conversational chapter per topic.
// Without LLM_API_KEY, it returns a deterministic local mock script.
func (g *GeminiLLM) GenerateScript(ctx context.Context, selected []domain.ScoredTopic) (string, []domain.ChapterDraft, error) {
	if strings.TrimSpace(os.Getenv("LLM_API_KEY")) == "" {
		greetingText, chapters := mockGenerateScript(selected)
		return greetingText, chapters, nil
	}

	requestBody, err := buildScriptRequest(selected)
	if err != nil {
		return "", nil, err
	}
	apiResponse, err := g.generateContent(ctx, requestBody)
	if err != nil {
		return "", nil, err
	}

	return parseScriptResult(apiResponse, selected, "Gemini")
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
	ResponseMIMEType string `json:"responseMimeType"`
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

func buildScoreRequest(topic domain.Topic, previousTopics []string) ([]byte, error) {
	prompt, err := buildScorePrompt(topic, previousTopics)
	if err != nil {
		return nil, err
	}

	request := generateContentRequest{
		Contents: []geminiContent{{Parts: []geminiPart{{Text: string(prompt)}}}},
		GenerationConfig: generationConfig{
			ResponseMIMEType: "application/json",
		},
	}
	return json.Marshal(request)
}

func buildScriptRequest(selected []domain.ScoredTopic) ([]byte, error) {
	prompt, err := buildScriptPrompt(selected)
	if err != nil {
		return nil, err
	}

	request := generateContentRequest{
		Contents: []geminiContent{{Parts: []geminiPart{{Text: string(prompt)}}}},
		GenerationConfig: generationConfig{
			ResponseMIMEType: "application/json",
		},
	}
	return json.Marshal(request)
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

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
	"unicode"

	"backend/internal/domain"
)

const defaultGeminiModel = "gemini-2.0-flash"

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

	var result scoreResult
	if err := json.Unmarshal([]byte(stripJSONFences(apiResponse)), &result); err != nil {
		return 0, false, fmt.Errorf("decode Gemini score response: %w", err)
	}
	if result.Score < 1 || result.Score > 5 {
		return 0, false, fmt.Errorf("Gemini returned invalid score %d: score must be between 1 and 5", result.Score)
	}
	if result.IsNewSnake != nil {
		result.IsNew = *result.IsNewSnake
	}
	return result.Score, result.IsNew, nil
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

	var result scriptResult
	if err := json.Unmarshal([]byte(stripJSONFences(apiResponse)), &result); err != nil {
		return "", nil, fmt.Errorf("decode Gemini script response: %w", err)
	}
	if len(result.Chapters) != len(selected) {
		return "", nil, fmt.Errorf("Gemini returned %d chapters for %d selected topics", len(result.Chapters), len(selected))
	}

	chapters := make([]domain.ChapterDraft, len(selected))
	for chapterIndex, generatedChapter := range result.Chapters {
		if len(generatedChapter.Lines) == 0 {
			return "", nil, fmt.Errorf("Gemini returned an empty chapter at index %d", chapterIndex)
		}
		lines := make([]domain.Line, len(generatedChapter.Lines))
		for lineIndex, line := range generatedChapter.Lines {
			speaker := strings.TrimSpace(line.Speaker)
			if speaker != "A" && speaker != "B" {
				return "", nil, fmt.Errorf("Gemini returned invalid speaker %q at chapter %d, line %d", line.Speaker, chapterIndex, lineIndex)
			}
			if strings.TrimSpace(line.Text) == "" {
				return "", nil, fmt.Errorf("Gemini returned an empty line at chapter %d, line %d", chapterIndex, lineIndex)
			}
			lines[lineIndex] = domain.Line{Speaker: speaker, Text: strings.TrimSpace(line.Text)}
		}
		chapters[chapterIndex] = domain.ChapterDraft{
			ScoredTopic: selected[chapterIndex],
			Lines:       lines,
		}
	}
	return strings.TrimSpace(result.GreetingText), chapters, nil
}

type scoreResult struct {
	Score      int   `json:"score"`
	IsNew      bool  `json:"isNew"`
	IsNewSnake *bool `json:"is_new"`
}

type scriptResult struct {
	GreetingText string          `json:"greetingText"`
	Chapters     []scriptChapter `json:"chapters"`
}

type scriptChapter struct {
	Lines []domain.Line `json:"lines"`
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
	previous, err := json.Marshal(previousTopics)
	if err != nil {
		return nil, err
	}
	prompt := fmt.Sprintf(`Evaluate the importance of this news topic for a personal AI radio program.
Return only a JSON object with this exact shape: {"score": 1, "isNew": true}.
score must be an integer from 1 (least important) to 5 (most important).
Set isNew to true when the topic is not substantially covered by any previous topic.

Title: %s
Article body (beginning): %s
Related article count: %d
Previous topic titles: %s`, topic.Primary.Title, truncateRunes(topic.Primary.Body, 1200), topic.RelatedCount, previous)

	request := generateContentRequest{
		Contents: []geminiContent{{Parts: []geminiPart{{Text: prompt}}}},
		GenerationConfig: generationConfig{
			ResponseMIMEType: "application/json",
		},
	}
	return json.Marshal(request)
}

func buildScriptRequest(selected []domain.ScoredTopic) ([]byte, error) {
	type scriptTopic struct {
		Position        int    `json:"position"`
		Title           string `json:"title"`
		Body            string `json:"body"`
		SourceName      string `json:"sourceName"`
		SourceURL       string `json:"sourceURL"`
		RelatedCount    int    `json:"relatedCount"`
		ImportanceScore int    `json:"importanceScore"`
		IsNew           bool   `json:"isNew"`
	}

	input := make([]scriptTopic, 0, len(selected))
	for _, topic := range selected {
		input = append(input, scriptTopic{
			Position:        topic.Position,
			Title:           topic.Primary.Title,
			Body:            truncateRunes(topic.Primary.Body, 1200),
			SourceName:      topic.Primary.SourceName,
			SourceURL:       topic.Primary.SourceURL,
			RelatedCount:    topic.RelatedCount,
			ImportanceScore: topic.ImportanceScore,
			IsNew:           topic.IsNew,
		})
	}
	topics, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	changeCount := countNewTopics(selected)
	if changeCount == 0 {
		changeCount = len(selected)
	}
	prompt := fmt.Sprintf(`Generate a natural, conversational Japanese radio script from the selected news topics.
Return only JSON with this exact shape:
{"greetingText":"...","chapters":[{"lines":[{"speaker":"A","text":"..."}]}]}
Create exactly one chapter for each input topic, in the same order.
Each chapter must contain 2 to 4 lines, alternating speakers A and B.
The greeting is separate from the chapters and must mention that there are %d change/new topics.
Include a natural transition into each topic and a brief closing sentence in the final chapter.
Do not include a user's display name. Use only speaker values "A" or "B".

Selected topics:
%s`, changeCount, topics)

	request := generateContentRequest{
		Contents: []geminiContent{{Parts: []geminiPart{{Text: prompt}}}},
		GenerationConfig: generationConfig{
			ResponseMIMEType: "application/json",
		},
	}
	return json.Marshal(request)
}

func (g *GeminiLLM) generateContent(ctx context.Context, requestBody []byte) (string, error) {
	apiKey := strings.TrimSpace(os.Getenv("LLM_API_KEY"))
	model := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
	if model == "" {
		model = defaultGeminiModel
	}
	endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", url.PathEscape(model))
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	query := parsedEndpoint.Query()
	query.Set("key", apiKey)
	parsedEndpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedEndpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")

	client := g.client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("Gemini returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var result generateContentResponse
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
}

func mockScoreTopic(topic domain.Topic, _ []string) int {
	score := 1
	switch {
	case topic.RelatedCount >= 8:
		score = 5
	case topic.RelatedCount >= 5:
		score = 4
	case topic.RelatedCount >= 3:
		score = 3
	case topic.RelatedCount >= 2:
		score = 2
	}
	return score
}

func mockIsNew(topic domain.Topic, previousTopics []string) bool {
	normalizedTitle := normalizeTopicTitle(topic.Primary.Title)
	if normalizedTitle == "" {
		return true
	}
	for _, previous := range previousTopics {
		if normalizedTitle == normalizeTopicTitle(previous) {
			return false
		}
	}
	return true
}

func mockGenerateScript(selected []domain.ScoredTopic) (string, []domain.ChapterDraft) {
	if len(selected) == 0 {
		return "", nil
	}
	changeCount := countNewTopics(selected)
	if changeCount == 0 {
		changeCount = len(selected)
	}
	greeting := fmt.Sprintf("[MOCK] 今日は%d件の差分トピックについてお伝えします。", changeCount)

	chapters := make([]domain.ChapterDraft, 0, len(selected))
	for _, topic := range selected {
		chapters = append(chapters, domain.ChapterDraft{
			ScoredTopic: topic,
			Lines: []domain.Line{
				{Speaker: "A", Text: fmt.Sprintf("[MOCK] まず、「%s」についてお伝えします。", topic.Primary.Title)},
				{Speaker: "B", Text: fmt.Sprintf("[MOCK] 関連する記事は%d件あり、ポイントを簡単に紹介します。", topic.RelatedCount)},
				{Speaker: "A", Text: fmt.Sprintf("[MOCK] 詳細は元記事（%s）をご確認ください。", topic.Primary.SourceURL)},
			},
		})
	}
	return greeting, chapters
}

func countNewTopics(selected []domain.ScoredTopic) int {
	count := 0
	for _, topic := range selected {
		if topic.IsNew {
			count++
		}
	}
	return count
}

func normalizeTopicTitle(title string) string {
	var normalized strings.Builder
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			normalized.WriteRune(r)
		}
	}
	return normalized.String()
}

func truncateRunes(value string, maxLength int) string {
	runes := []rune(value)
	if len(runes) <= maxLength {
		return value
	}
	return string(runes[:maxLength])
}

func stripJSONFences(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "```") {
		return value
	}
	newline := strings.IndexByte(value, '\n')
	if newline < 0 {
		return value
	}
	value = value[newline+1:]
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "```")
	return strings.TrimSpace(value)
}

var _ LLM = (*GeminiLLM)(nil)

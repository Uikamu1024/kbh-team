package llm

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"backend/internal/domain"
)

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

func buildScorePrompt(topic domain.Topic, previousTopics []string) ([]byte, error) {
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
	return []byte(prompt), nil
}

func buildScriptPrompt(selected []domain.ScoredTopic) ([]byte, error) {
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
	return []byte(prompt), nil
}

func parseScoreResult(apiResponse, provider string) (int, bool, error) {
	var result scoreResult
	if err := json.Unmarshal([]byte(stripJSONFences(apiResponse)), &result); err != nil {
		return 0, false, fmt.Errorf("decode %s score response: %w", provider, err)
	}
	if result.Score < 1 || result.Score > 5 {
		return 0, false, fmt.Errorf("%s returned invalid score %d: score must be between 1 and 5", provider, result.Score)
	}
	if result.IsNewSnake != nil {
		result.IsNew = *result.IsNewSnake
	}
	return result.Score, result.IsNew, nil
}

func parseScriptResult(apiResponse string, selected []domain.ScoredTopic, provider string) (string, []domain.ChapterDraft, error) {
	var result scriptResult
	if err := json.Unmarshal([]byte(stripJSONFences(apiResponse)), &result); err != nil {
		return "", nil, fmt.Errorf("decode %s script response: %w", provider, err)
	}
	if len(result.Chapters) < len(selected) {
		return "", nil, fmt.Errorf("%s returned %d chapters for %d selected topics", provider, len(result.Chapters), len(selected))
	}
	if len(result.Chapters) > len(selected) {
		result.Chapters = result.Chapters[:len(selected)]
	}

	chapters := make([]domain.ChapterDraft, len(selected))
	for chapterIndex, generatedChapter := range result.Chapters {
		if len(generatedChapter.Lines) == 0 {
			return "", nil, fmt.Errorf("%s returned an empty chapter at index %d", provider, chapterIndex)
		}
		lines := make([]domain.Line, len(generatedChapter.Lines))
		for lineIndex, line := range generatedChapter.Lines {
			speaker := strings.TrimSpace(line.Speaker)
			if speaker != "A" && speaker != "B" {
				return "", nil, fmt.Errorf("%s returned invalid speaker %q at chapter %d, line %d", provider, line.Speaker, chapterIndex, lineIndex)
			}
			if strings.TrimSpace(line.Text) == "" {
				return "", nil, fmt.Errorf("%s returned an empty line at chapter %d, line %d", provider, chapterIndex, lineIndex)
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

// IsTopicNew reports whether a topic title is absent from the previous
// program after case and punctuation normalization.
func IsTopicNew(topic domain.Topic, previousTopics []string) bool {
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

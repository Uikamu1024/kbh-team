package llm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"backend/internal/domain"
)

// maxMalformedJSONRetries bounds retries for a single prompt whose response
// fails to parse as the expected JSON shape. Reasoning models sometimes spend
// their output budget on internal reasoning and cut the visible JSON short;
// the failure is not consistent across attempts, so a bounded retry recovers
// most of the time without guessing at provider-specific token/reasoning-
// effort knobs.
const maxMalformedJSONRetries = 2

// promptSender sends a single raw prompt to a provider's chat/generation
// endpoint and returns the raw text response. Each provider (Gemini,
// OpenAI-compatible) implements this once; the one-topic-per-call helpers
// below are shared regardless of the underlying request/response shape.
type promptSender func(ctx context.Context, prompt string) (string, error)

// navLineRE matches a line that consists entirely of one or more Markdown
// links/images with no other text — the shape jina.ai Reader produces for
// site navigation menus, login links, and category lists that precede the
// actual article body on many news sites.
var navLineRE = regexp.MustCompile(`^(?:\[!\[[^\]]*\]\([^)]*\)\]\([^)]*\)|\[[^\]]*\]\([^)]*\)|!\[[^\]]*\]\([^)]*\))+$`)

// stripNavigationLines removes pure navigation-link lines from a jina.ai
// Reader Markdown body before it is sent to the LLM.
//
// Found in production: many sites' Reader output front-loads a large block
// of site-navigation links (header menu, login, category list) before the
// article text begins. The article body used to be truncated to a fixed
// character count for the prompt, which for some articles fell entirely
// within this navigation block, leaving the LLM with zero actual article
// content for that topic (verified against a real cached article: the
// first 1200 characters were 100% navigation links). The body is no longer
// truncated at all, but this stripping is still worth keeping since
// navigation noise adds nothing but token cost.
func stripNavigationLines(body string) string {
	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || navLineRE.MatchString(trimmed) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// scoreTopicViaPrompt scores one topic with a single prompt/response
// round trip, retrying on malformed JSON.
func scoreTopicViaPrompt(ctx context.Context, send promptSender, provider string, topic domain.Topic, previousTopics []string) (int, bool, error) {
	prompt, err := buildScorePrompt(topic, previousTopics)
	if err != nil {
		return 0, false, err
	}

	var lastErr error
	for attempt := 1; attempt <= maxMalformedJSONRetries; attempt++ {
		response, err := send(ctx, prompt)
		if err != nil {
			return 0, false, err
		}
		score, isNew, err := parseScoreResult(response, provider)
		if err == nil {
			return score, isNew, nil
		}
		lastErr = err
	}
	return 0, false, lastErr
}

// generateScriptViaChapters builds the program script one LLM call at a
// time — one call for the greeting, then exactly one call per selected
// topic for its chapter — instead of asking for the whole program's JSON in
// a single response.
//
// KNOWN LIMITATION (fixed 2026-08-29): the previous design batched every
// selected topic's full article body into one prompt and expected one
// response containing every chapter's JSON. This scaled badly: with more
// than a handful of chapters (verified with 14), or once article bodies
// were no longer truncated, the combined prompt/response grew large enough
// that the model's JSON output was reliably cut off before completing,
// failing even after retries ("unexpected end of JSON input"). Scoring
// already worked one topic per call and never had this problem. Chapters
// now follow the same one-call-per-topic pattern: each call is small,
// independent, and trivially retryable, and a full untruncated article body
// can be included without the request size compounding across chapters.
func generateScriptViaChapters(ctx context.Context, send promptSender, provider string, selected []domain.SelectedTopic) (string, []domain.ChapterDraft, error) {
	if len(selected) == 0 {
		return "", nil, nil
	}

	changeCount := countNewTopics(selected)
	if changeCount == 0 {
		changeCount = len(selected)
	}

	greetingText, err := generateGreetingViaPrompt(ctx, send, provider, changeCount)
	if err != nil {
		return "", nil, fmt.Errorf("generate greeting: %w", err)
	}

	chapters := make([]domain.ChapterDraft, len(selected))
	for index, topic := range selected {
		lines, err := generateChapterViaPrompt(ctx, send, provider, topic, index == len(selected)-1)
		if err != nil {
			return "", nil, fmt.Errorf("generate chapter %d: %w", index, err)
		}
		chapters[index] = domain.ChapterDraft{SelectedTopic: topic, Lines: lines}
	}
	return greetingText, chapters, nil
}

func generateGreetingViaPrompt(ctx context.Context, send promptSender, provider string, changeCount int) (string, error) {
	prompt := buildGreetingPrompt(changeCount)

	var lastErr error
	for attempt := 1; attempt <= maxMalformedJSONRetries; attempt++ {
		response, err := send(ctx, prompt)
		if err != nil {
			return "", err
		}
		greetingText, err := parseGreetingResult(response, provider)
		if err == nil {
			return greetingText, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func generateChapterViaPrompt(ctx context.Context, send promptSender, provider string, topic domain.SelectedTopic, isLast bool) ([]domain.Line, error) {
	prompt := buildChapterPrompt(topic, isLast)

	var lastErr error
	for attempt := 1; attempt <= maxMalformedJSONRetries; attempt++ {
		response, err := send(ctx, prompt)
		if err != nil {
			return nil, err
		}
		lines, err := parseChapterResult(response, provider)
		if err == nil {
			return lines, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// structuredPromptSender sends one prompt requesting a structured-output
// response constrained by a caller-provided JSON Schema, and returns the raw
// text response. Each provider builds the schema in its own dialect (Gemini's
// responseSchema vs. OpenAI-style json_schema) and implements this once; the
// shared extraction/duplicate-detection helpers below only depend on the
// resulting text following the field names described in the prompt.
type structuredPromptSender func(ctx context.Context, prompt string, schema map[string]any) (string, error)

// abbreviationThreshold is the article body length (in runes), above which
// the ingest job asks the LLM to also produce an abbreviatedBody
// (backend/docs/generation/02-ingestion.md step 5). Bodies at or below this
// length are used as-is; no truncation is applied to them.
const abbreviationThreshold = 2500

// shortenedTitleMaxRunes bounds the LLM-generated UI display title
// (backend/docs/generation/02-ingestion.md step 5). Enforced defensively in
// Go in case the model overruns the length instructed in the prompt.
const shortenedTitleMaxRunes = 45

// extractMetadataViaPrompt asks the LLM to classify tags and extract
// UI/summary metadata for one article, retrying on malformed JSON.
func extractMetadataViaPrompt(ctx context.Context, send structuredPromptSender, provider, title, body string) (domain.ArticleMetadata, error) {
	cleanBody := stripNavigationLines(body)
	prompt := buildMetadataPrompt(title, cleanBody)
	schema := metadataResponseSchema()

	var lastErr error
	for attempt := 1; attempt <= maxMalformedJSONRetries; attempt++ {
		response, err := send(ctx, prompt, schema)
		if err != nil {
			return domain.ArticleMetadata{}, err
		}
		metadata, err := parseMetadataResult(response, provider)
		if err == nil {
			return metadata, nil
		}
		lastErr = err
	}
	return domain.ArticleMetadata{}, lastErr
}

// metadataResponseSchema is the article-metadata response shape as a
// standard JSON Schema (draft 2020-12 subset), matching
// backend/docs/generation/02-ingestion.md step 5's JSON Schema. Nullable
// fields use a ["string","null"] type union, which OpenAI-compatible
// providers' strict json_schema mode expects directly; Gemini's dialect
// (responseSchema) is derived from this same map by toGeminiSchema in
// gemini.go, since Gemini uses "nullable": true instead of a type union.
func metadataResponseSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"shortenedTitle": map[string]any{"type": "string"},
			"tags": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string", "enum": domain.PresetTags},
			},
			"author":          map[string]any{"type": []string{"string", "null"}},
			"publishedAt":     map[string]any{"type": []string{"string", "null"}},
			"abbreviatedBody": map[string]any{"type": []string{"string", "null"}},
		},
		"required":             []string{"shortenedTitle", "tags", "author", "publishedAt", "abbreviatedBody"},
		"additionalProperties": false,
	}
}

func buildMetadataPrompt(title, body string) string {
	tagList, _ := json.Marshal(domain.PresetTags)
	return fmt.Sprintf(`Extract structured metadata for this news article.
Return only a JSON object with this exact shape:
{"shortenedTitle":"...","tags":["..."],"author":null,"publishedAt":null,"abbreviatedBody":null}

shortenedTitle: a short Japanese UI display title, at most 45 characters.
tags: zero or more values, each one must be exactly one of this fixed list (do not invent new tags, do not translate or reword them): %s
author: the article's author name if it can be found in the body, otherwise null.
publishedAt: an ISO8601 timestamp if a publish date/time can be confirmed from the body text itself, otherwise null.
abbreviatedBody: if the article body below is longer than %d characters, a faithful Japanese summary of it; otherwise null.

Title: %s
Article body: %s`, tagList, abbreviationThreshold, title, body)
}

type metadataResult struct {
	ShortenedTitle  string   `json:"shortenedTitle"`
	Tags            []string `json:"tags"`
	Author          *string  `json:"author"`
	PublishedAt     *string  `json:"publishedAt"`
	AbbreviatedBody *string  `json:"abbreviatedBody"`
}

func parseMetadataResult(apiResponse, provider string) (domain.ArticleMetadata, error) {
	var result metadataResult
	if err := json.Unmarshal([]byte(stripJSONFences(apiResponse)), &result); err != nil {
		return domain.ArticleMetadata{}, fmt.Errorf("decode %s metadata response: %w", provider, err)
	}

	metadata := domain.ArticleMetadata{
		ShortenedTitle: truncateRunes(strings.TrimSpace(result.ShortenedTitle), shortenedTitleMaxRunes),
		Tags:           filterPresetTags(result.Tags),
	}
	if result.Author != nil {
		metadata.Author = strings.TrimSpace(*result.Author)
	}
	if result.AbbreviatedBody != nil {
		metadata.AbbreviatedBody = strings.TrimSpace(*result.AbbreviatedBody)
	}
	if result.PublishedAt != nil && strings.TrimSpace(*result.PublishedAt) != "" {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*result.PublishedAt)); err == nil {
			metadata.PublishedAt = &parsed
		}
		// An unparsable publishedAt is treated the same as absent (nil): the
		// caller falls back to the feed's RSS pubDate, per
		// backend/docs/generation/02-ingestion.md step 5.
	}
	return metadata, nil
}

// filterPresetTags keeps only tags that are exact, case-sensitive matches
// against domain.PresetTags, guarding against a provider whose structured
// output enforcement is soft (e.g. response_format: json_object) returning a
// tag outside the fixed list.
func filterPresetTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(domain.PresetTags))
	for _, tag := range domain.PresetTags {
		allowed[tag] = true
	}
	filtered := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if allowed[tag] && !seen[tag] {
			filtered = append(filtered, tag)
			seen[tag] = true
		}
	}
	return filtered
}

func truncateRunes(value string, maxRunes int) string {
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}

// mockExtractMetadata is used when no API key is configured. It performs no
// classification (empty tags, no author/publishedAt override) and never
// abbreviates, matching the conservative fallback used by mockScoreTopic and
// mockGenerateScript elsewhere in this file.
func mockExtractMetadata(title string) domain.ArticleMetadata {
	return domain.ArticleMetadata{
		ShortenedTitle: truncateRunes(title, shortenedTitleMaxRunes),
	}
}

// detectDuplicatesViaPrompt asks the LLM, in one call, to group newTitles
// among themselves and against existingGroups
// (backend/docs/generation/02-ingestion.md step 6), then resolves the
// response into concrete topic_group_id values. The LLM is never asked to
// invent UUIDs itself (models are unreliable at producing well-formed,
// non-colliding UUIDs); it instead returns an index-based classification
// that Go resolves against the real existing IDs (validated against
// existingGroups, guarding against hallucinated IDs) or mints fresh ones for.
func detectDuplicatesViaPrompt(ctx context.Context, send structuredPromptSender, provider string, newTitles []string, existingGroups []domain.ExistingTopicGroup) ([]domain.DuplicateAssignment, error) {
	if len(newTitles) == 0 {
		return nil, nil
	}
	prompt := buildDuplicatePrompt(newTitles, existingGroups)
	schema := duplicateResponseSchema()

	var lastErr error
	for attempt := 1; attempt <= maxMalformedJSONRetries; attempt++ {
		response, err := send(ctx, prompt, schema)
		if err != nil {
			return nil, err
		}
		result, err := parseDuplicateResult(response, provider)
		if err == nil {
			return resolveDuplicateAssignments(newTitles, existingGroups, result)
		}
		lastErr = err
	}
	return nil, lastErr
}

// duplicateResponseSchema is the duplicate-detection response shape as a
// standard JSON Schema, mirrored into Gemini's dialect by toGeminiSchema.
func duplicateResponseSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"assignments": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"index":                map[string]any{"type": "integer"},
						"matchType":            map[string]any{"type": "string", "enum": []string{"existing", "batch", "none"}},
						"existingTopicGroupId": map[string]any{"type": []string{"string", "null"}},
						"batchGroupKey":        map[string]any{"type": []string{"string", "null"}},
					},
					"required":             []string{"index", "matchType", "existingTopicGroupId", "batchGroupKey"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"assignments"},
		"additionalProperties": false,
	}
}

func buildDuplicatePrompt(newTitles []string, existingGroups []domain.ExistingTopicGroup) string {
	var newList strings.Builder
	for index, title := range newTitles {
		fmt.Fprintf(&newList, "%d: %s\n", index, title)
	}

	var existingList strings.Builder
	if len(existingGroups) == 0 {
		existingList.WriteString("(none)\n")
	} else {
		for _, group := range existingGroups {
			fmt.Fprintf(&existingList, "%s: %s\n", group.TopicGroupID, group.Title)
		}
	}

	return fmt.Sprintf(`You are deduplicating news articles by topic for a personal AI radio program.
For each new article title below (identified by its index), decide exactly one of:
- "existing": it covers the same story as one of the existing groups listed below. Set existingTopicGroupId to that group's id (copy it exactly).
- "batch": it covers the same story as one or more OTHER new titles below (not an existing group). Set batchGroupKey to any string shared by every new title in that same story; use a different batchGroupKey per distinct story.
- "none": it does not match any existing group or any other new title.

Return only a JSON object with this exact shape:
{"assignments":[{"index":0,"matchType":"none","existingTopicGroupId":null,"batchGroupKey":null}, ...]}
Include exactly one assignment per new title index, covering every index from 0 to %d.

New article titles (index: title):
%s
Existing group titles (id: title), within the last 14 days:
%s`, len(newTitles)-1, newList.String(), existingList.String())
}

type duplicateAssignmentResult struct {
	Index                int     `json:"index"`
	MatchType            string  `json:"matchType"`
	ExistingTopicGroupID *string `json:"existingTopicGroupId"`
	BatchGroupKey        *string `json:"batchGroupKey"`
}

type duplicateDetectionResult struct {
	Assignments []duplicateAssignmentResult `json:"assignments"`
}

func parseDuplicateResult(apiResponse, provider string) (duplicateDetectionResult, error) {
	var result duplicateDetectionResult
	if err := json.Unmarshal([]byte(stripJSONFences(apiResponse)), &result); err != nil {
		return duplicateDetectionResult{}, fmt.Errorf("decode %s duplicate-detection response: %w", provider, err)
	}
	if len(result.Assignments) == 0 {
		return duplicateDetectionResult{}, fmt.Errorf("%s returned no duplicate-detection assignments", provider)
	}
	return result, nil
}

// resolveDuplicateAssignments turns the LLM's index-based classification
// into one domain.DuplicateAssignment per newTitles entry, in the same
// order. A missing, invalid, or hallucinated assignment (unknown index,
// existingTopicGroupId not present in existingGroups) safely falls back to
// "none" (a freshly minted group), since silently dropping an article would
// be worse than under-merging it with a related story.
func resolveDuplicateAssignments(newTitles []string, existingGroups []domain.ExistingTopicGroup, result duplicateDetectionResult) ([]domain.DuplicateAssignment, error) {
	existingIDs := make(map[string]bool, len(existingGroups))
	for _, group := range existingGroups {
		existingIDs[group.TopicGroupID] = true
	}
	byIndex := make(map[int]duplicateAssignmentResult, len(result.Assignments))
	for _, assignment := range result.Assignments {
		byIndex[assignment.Index] = assignment
	}

	batchGroupIDs := make(map[string]string)
	assignments := make([]domain.DuplicateAssignment, len(newTitles))
	for index, title := range newTitles {
		raw, ok := byIndex[index]
		groupID := ""
		if ok {
			switch raw.MatchType {
			case "existing":
				if raw.ExistingTopicGroupID != nil && existingIDs[*raw.ExistingTopicGroupID] {
					groupID = *raw.ExistingTopicGroupID
				}
			case "batch":
				if raw.BatchGroupKey != nil && strings.TrimSpace(*raw.BatchGroupKey) != "" {
					key := strings.TrimSpace(*raw.BatchGroupKey)
					if id, seen := batchGroupIDs[key]; seen {
						groupID = id
					} else {
						newID, err := newUUIDv4()
						if err != nil {
							return nil, err
						}
						batchGroupIDs[key] = newID
						groupID = newID
					}
				}
			}
		}
		if groupID == "" {
			newID, err := newUUIDv4()
			if err != nil {
				return nil, err
			}
			groupID = newID
		}
		assignments[index] = domain.DuplicateAssignment{Title: title, TopicGroupID: groupID}
	}
	return assignments, nil
}

// mockDetectDuplicates is used when no API key is configured. It performs no
// grouping: every new title gets its own freshly minted group, matching the
// conservative fallback used elsewhere in this file when the LLM is
// unavailable.
func mockDetectDuplicates(newTitles []string) ([]domain.DuplicateAssignment, error) {
	assignments := make([]domain.DuplicateAssignment, len(newTitles))
	for index, title := range newTitles {
		id, err := newUUIDv4()
		if err != nil {
			return nil, err
		}
		assignments[index] = domain.DuplicateAssignment{Title: title, TopicGroupID: id}
	}
	return assignments, nil
}

// newUUIDv4 generates a random (v4) UUID. Duplicated locally rather than
// shared, matching the existing small per-package UUID helpers in
// internal/api/ids.go and internal/db/users.go.
func newUUIDv4() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	var encoded [36]byte
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded[:]), nil
}

type scoreResult struct {
	Score      int   `json:"score"`
	IsNew      bool  `json:"isNew"`
	IsNewSnake *bool `json:"is_new"`
}

type greetingResult struct {
	GreetingText string `json:"greetingText"`
}

type chapterResult struct {
	Lines []domain.Line `json:"lines"`
}

func buildScorePrompt(topic domain.Topic, previousTopics []string) (string, error) {
	previous, err := json.Marshal(previousTopics)
	if err != nil {
		return "", err
	}
	prompt := fmt.Sprintf(`Evaluate the importance of this news topic for a personal AI radio program.
Return only a JSON object with this exact shape: {"score": 1, "isNew": true}.
score must be an integer from 1 (least important) to 5 (most important).
Set isNew to true when the topic is not substantially covered by any previous topic.

Title: %s
Article body (beginning): %s
Related article count: %d
Previous topic titles: %s`, topic.Primary.Title, stripNavigationLines(topic.Primary.Body), topic.RelatedCount, previous)
	return prompt, nil
}

func buildGreetingPrompt(changeCount int) string {
	return fmt.Sprintf(`Generate a short, natural Japanese greeting for the start of a personal AI radio program.
Return only a JSON object with this exact shape: {"greetingText": "..."}.
The greeting must mention that there are %d change/new topics in today's program.
Do not include a user's display name.`, changeCount)
}

// buildChapterPrompt includes the article's metadata (shortenedTitle, tags,
// author, sourceName, sourceURL, publishedAt) alongside its body
// (backend/docs/generation/03-selection.md: "プロンプトには記事のメタデータ
// ...も含める"). Fields populated only by the ingest job's LLM metadata
// extraction (backend/docs/generation/02-ingestion.md step 5) — shortenedTitle,
// tags, author, abbreviatedBody — are empty for topics from the live-fetch
// cmd/demo path, which does not run that extraction; empty lines are omitted
// rather than sent to the model as literal blanks.
func buildChapterPrompt(topic domain.SelectedTopic, isLast bool) string {
	transitionInstruction := "Include a natural spoken transition into this topic."
	if isLast {
		transitionInstruction += " End the chapter with a brief closing sentence for the whole program."
	}

	body := topic.Primary.Body
	if strings.TrimSpace(topic.Primary.AbbreviatedBody) != "" {
		// Prefer the ingest job's LLM-summarized body when the original body
		// was long enough to warrant one (backend/docs/generation/02-ingestion.md
		// step 5); otherwise the untruncated original body is used as-is.
		body = topic.Primary.AbbreviatedBody
	}

	var metadata strings.Builder
	fmt.Fprintf(&metadata, "Title: %s\n", topic.Primary.Title)
	if strings.TrimSpace(topic.Primary.ShortenedTitle) != "" {
		fmt.Fprintf(&metadata, "Shortened title: %s\n", topic.Primary.ShortenedTitle)
	}
	if len(topic.Primary.Tags) > 0 {
		fmt.Fprintf(&metadata, "Tags: %s\n", strings.Join(topic.Primary.Tags, ", "))
	}
	if strings.TrimSpace(topic.Primary.Author) != "" {
		fmt.Fprintf(&metadata, "Author: %s\n", topic.Primary.Author)
	}
	fmt.Fprintf(&metadata, "Source: %s (%s)\n", topic.Primary.SourceName, topic.Primary.SourceURL)
	if !topic.Primary.PublishedAt.IsZero() {
		fmt.Fprintf(&metadata, "Published at: %s\n", topic.Primary.PublishedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(&metadata, "Related article count: %d\n", topic.RelatedCount)

	return fmt.Sprintf(`Generate one chapter of a natural, conversational Japanese radio script covering this single news topic.
Return only JSON with this exact shape: {"lines":[{"speaker":"A","text":"..."}]}.
The chapter must contain 2 to 4 lines, alternating speakers A and B.
%s
Do not include a user's display name. Use only speaker values "A" or "B".

%sArticle body: %s`, transitionInstruction, metadata.String(), stripNavigationLines(body))
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

func parseGreetingResult(apiResponse, provider string) (string, error) {
	var result greetingResult
	if err := json.Unmarshal([]byte(stripJSONFences(apiResponse)), &result); err != nil {
		return "", fmt.Errorf("decode %s greeting response: %w", provider, err)
	}
	if strings.TrimSpace(result.GreetingText) == "" {
		return "", fmt.Errorf("%s returned an empty greeting", provider)
	}
	return strings.TrimSpace(result.GreetingText), nil
}

func parseChapterResult(apiResponse, provider string) ([]domain.Line, error) {
	var result chapterResult
	if err := json.Unmarshal([]byte(stripJSONFences(apiResponse)), &result); err != nil {
		return nil, fmt.Errorf("decode %s chapter response: %w", provider, err)
	}
	if len(result.Lines) == 0 {
		return nil, fmt.Errorf("%s returned an empty chapter", provider)
	}
	lines := make([]domain.Line, len(result.Lines))
	for lineIndex, line := range result.Lines {
		speaker := strings.TrimSpace(line.Speaker)
		if speaker != "A" && speaker != "B" {
			return nil, fmt.Errorf("%s returned invalid speaker %q at line %d", provider, line.Speaker, lineIndex)
		}
		if strings.TrimSpace(line.Text) == "" {
			return nil, fmt.Errorf("%s returned an empty line at line %d", provider, lineIndex)
		}
		lines[lineIndex] = domain.Line{Speaker: speaker, Text: strings.TrimSpace(line.Text)}
	}
	return lines, nil
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

func mockGenerateScript(selected []domain.SelectedTopic) (string, []domain.ChapterDraft) {
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
			SelectedTopic: topic,
			Lines: []domain.Line{
				{Speaker: "A", Text: fmt.Sprintf("[MOCK] まず、「%s」についてお伝えします。", topic.Primary.Title)},
				{Speaker: "B", Text: fmt.Sprintf("[MOCK] 関連する記事は%d件あり、ポイントを簡単に紹介します。", topic.RelatedCount)},
				{Speaker: "A", Text: fmt.Sprintf("[MOCK] 詳細は元記事（%s）をご確認ください。", topic.Primary.SourceURL)},
			},
		})
	}
	return greeting, chapters
}

func countNewTopics(selected []domain.SelectedTopic) int {
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

package llm

import (
	"context"

	"backend/internal/domain"
)

// LLM scores topics, generates the script for selected topics, and (for the
// ingest job, backend/docs/generation/02-ingestion.md) extracts structured
// article metadata and detects duplicate topics among newly-fetched articles.
type LLM interface {
	// ScoreTopic scores one topic and determines whether it is new compared with previous topics.
	ScoreTopic(ctx context.Context, topic domain.Topic, previousTopics []string) (score int, isNew bool, err error)
	// GenerateScript generates the greeting and chapter drafts for selected
	// topics. Used by both the live-fetch cmd/demo path (score.go converts its
	// ScoredTopic ranking to SelectedTopic first) and the cache-backed
	// selection path (select.go), neither of which carries an importance score.
	GenerateScript(ctx context.Context, selected []domain.SelectedTopic) (greetingText string, chapters []domain.ChapterDraft, err error)
	// ExtractMetadata asks the LLM to classify tags (constrained to
	// domain.PresetTags) and extract UI/summary metadata for one article via
	// structured output (backend/docs/generation/02-ingestion.md step 5).
	ExtractMetadata(ctx context.Context, title, body string) (domain.ArticleMetadata, error)
	// DetectDuplicates groups newTitles among themselves and against
	// existingGroups in a single structured-output call, returning one
	// DuplicateAssignment per newTitles entry in the same order
	// (backend/docs/generation/02-ingestion.md step 6).
	DetectDuplicates(ctx context.Context, newTitles []string, existingGroups []domain.ExistingTopicGroup) ([]domain.DuplicateAssignment, error)
}

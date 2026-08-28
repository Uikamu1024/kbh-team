package llm

import (
	"context"

	"backend/internal/domain"
)

// LLM scores topics and generates the script for selected topics.
type LLM interface {
	// ScoreTopic scores one topic and determines whether it is new compared with previous topics.
	ScoreTopic(ctx context.Context, topic domain.Topic, previousTopics []string) (score int, isNew bool, err error)
	// GenerateScript generates the greeting and chapter drafts for selected topics.
	GenerateScript(ctx context.Context, selected []domain.ScoredTopic) (greetingText string, chapters []domain.ChapterDraft, err error)
}

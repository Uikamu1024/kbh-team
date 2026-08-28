package pipeline

import (
	"context"
	"fmt"

	"backend/internal/domain"
	"backend/internal/providers/llm"
)

// GenerateScript delegates script generation to the configured LLM.
func GenerateScript(ctx context.Context, generator llm.LLM, selected []domain.ScoredTopic) (string, []domain.ChapterDraft, error) {
	if generator == nil {
		return "", nil, fmt.Errorf("LLM is nil")
	}
	if len(selected) == 0 {
		return "", nil, nil
	}

	greetingText, chapters, err := generator.GenerateScript(ctx, selected)
	if err != nil {
		return "", nil, fmt.Errorf("generate script: %w", err)
	}
	return greetingText, chapters, nil
}

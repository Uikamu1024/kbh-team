package pipeline

import (
	"context"
	"fmt"
	"log"
	"sort"

	"backend/internal/domain"
	"backend/internal/providers/llm"
)

// assumedChapterSeconds estimates how long one chapter's spoken audio will
// be, for budgeting how many topics to select against lengthMinutes.
//
// KNOWN LIMITATION (fixed 2026-08-29): this used to estimate duration from
// Primary.Body's character count (assuming ~325 Japanese characters/minute).
// That is wrong: Primary.Body is the raw scraped article (jina.ai Markdown,
// including navigation/image-alt-text noise), not what gets read aloud. The
// actual spoken content is script.go's LLM-generated 2-4 line dialogue for
// the chapter, which bears no relationship to the source article's length.
// Verified in production: a single cached article's raw body (4,000-22,000+
// chars) alone exceeded any length_minutes budget, so selectTopics always
// picked exactly 1 topic regardless of setting, while the real synthesized
// chapter (a short LLM summary) only ran ~20-50 seconds — producing programs
// far shorter than length_minutes requested. A fixed per-chapter estimate,
// based on observed real chapter durations, avoids depending on unrelated
// source-article length until script duration is known.
const assumedChapterSeconds = 45

// ScoreAndSelect scores topics, orders them by importance, and selects topics
// that fit the requested program length. Only cmd/demo's live-fetch path uses
// this: the importance score itself is used here purely to decide ranking
// and the selection cutoff, then discarded — the returned SelectedTopic
// carries no score, matching the cache-backed selection path
// (backend/docs/generation/03-selection.md), since neither the API response
// nor the DB persist an importance score any more.
func ScoreAndSelect(ctx context.Context, scorer llm.LLM, topics []domain.Topic, lengthMinutes int, previousTopics []string) ([]domain.SelectedTopic, int, error) {
	if scorer == nil {
		return nil, 0, fmt.Errorf("LLM is nil")
	}
	if len(topics) == 0 {
		return nil, 0, nil
	}

	// A single topic failing to score (e.g. a transient LLM rate limit that
	// outlasted the provider's own retries) shouldn't discard every other
	// topic that scored successfully, so failures are skipped rather than
	// aborting the whole batch. Only return an error when nothing could be
	// scored at all.
	scored := make([]domain.ScoredTopic, 0, len(topics))
	for _, topic := range topics {
		score, isNew, err := scorer.ScoreTopic(ctx, topic, previousTopics)
		if err != nil {
			log.Printf("pipeline: skipping topic %q: score failed: %v", topic.Primary.Title, err)
			continue
		}
		scored = append(scored, domain.ScoredTopic{
			Topic:           topic,
			ImportanceScore: score,
			IsNew:           isNew,
		})
	}
	if len(scored) == 0 {
		return nil, 0, fmt.Errorf("score topics: all %d topics failed", len(topics))
	}

	ranked, changeCount := rankAndSelectTopics(scored, lengthMinutes)
	selected := make([]domain.SelectedTopic, len(ranked))
	for index, topic := range ranked {
		selected[index] = domain.SelectedTopic{Topic: topic.Topic, IsNew: topic.IsNew, Position: topic.Position}
	}
	return selected, changeCount, nil
}

func rankAndSelectTopics(scored []domain.ScoredTopic, lengthMinutes int) ([]domain.ScoredTopic, int) {
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].ImportanceScore != scored[j].ImportanceScore {
			return scored[i].ImportanceScore > scored[j].ImportanceScore
		}
		return scored[i].RelatedCount > scored[j].RelatedCount
	})

	selected := selectTopics(scored, lengthMinutes)
	changeCount := 0
	for index := range selected {
		selected[index].Position = index
		if selected[index].IsNew {
			changeCount++
		}
	}
	return selected, changeCount
}

func selectTopics(scored []domain.ScoredTopic, lengthMinutes int) []domain.ScoredTopic {
	if len(scored) == 0 {
		return nil
	}

	maxChapters := (lengthMinutes*60 + assumedChapterSeconds - 1) / assumedChapterSeconds
	if maxChapters < 1 {
		maxChapters = 1
	}
	if maxChapters > len(scored) {
		maxChapters = len(scored)
	}
	return append([]domain.ScoredTopic(nil), scored[:maxChapters]...)
}

package pipeline

import (
	"context"
	"fmt"
	"log"
	"sort"
	"unicode/utf8"

	"backend/internal/domain"
	"backend/internal/providers/llm"
)

// Japanese speech is roughly 300-350 characters per minute. The midpoint is
// used here as a simple estimate for the program's time budget.
const estimatedCharactersPerMinute = 325

// ScoreAndSelect scores topics, orders them by importance, and selects topics
// that fit the requested program length.
func ScoreAndSelect(ctx context.Context, scorer llm.LLM, topics []domain.Topic, lengthMinutes int, previousTopics []string) ([]domain.ScoredTopic, int, error) {
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
	return selected, changeCount, nil
}

func selectTopics(scored []domain.ScoredTopic, lengthMinutes int) []domain.ScoredTopic {
	if len(scored) == 0 {
		return nil
	}

	selected := make([]domain.ScoredTopic, 0, len(scored))
	budget := float64(lengthMinutes)
	usedMinutes := 0.0
	for _, topic := range scored {
		durationMinutes := float64(utf8.RuneCountInString(topic.Primary.Body)) / estimatedCharactersPerMinute
		if len(selected) > 0 && usedMinutes+durationMinutes > budget {
			break
		}
		selected = append(selected, topic)
		usedMinutes += durationMinutes
	}

	// Always retain the highest-scoring topic, even when it alone exceeds the budget.
	if len(selected) == 0 {
		selected = append(selected, scored[0])
	}
	return selected
}

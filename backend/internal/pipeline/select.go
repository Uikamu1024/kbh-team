package pipeline

import (
	"context"
	"errors"
	"fmt"

	"backend/internal/db"
	"backend/internal/domain"
	"backend/internal/providers/llm"
)

var (
	// ErrArticleCacheEmpty means no fresh cached article matches the requested tags.
	ErrArticleCacheEmpty = errors.New("article cache empty")
	// ErrNoUnseenArticles means matching fresh articles exist, but this user has
	// already received every matching topic group.
	ErrNoUnseenArticles = errors.New("no unseen articles")
)

// SelectCachedTopics gets fresh article groups matching userTags from the
// local cache (backend/docs/generation/03-selection.md), ordered by
// published_at DESC — no external communication or LLM call. An empty
// userID is used by the non-persistent demo endpoint and does not apply the
// seen-topic exclusion.
func SelectCachedTopics(ctx context.Context, database *db.DB, userTags []string, userID string) ([]domain.SelectedTopic, error) {
	if database == nil {
		return nil, fmt.Errorf("database is nil")
	}

	candidates, err := database.SelectCachedTopics(ctx, userTags, userID, false)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrArticleCacheEmpty
	}
	if userID == "" {
		return candidates, nil
	}

	unseen, err := database.SelectCachedTopics(ctx, userTags, userID, true)
	if err != nil {
		return nil, err
	}
	if len(unseen) == 0 {
		return nil, ErrNoUnseenArticles
	}
	return unseen, nil
}

// SelectAndRankCachedTopics selects cached topics matching userTags and
// applies the program-length budget, without calling an LLM
// (backend/docs/generation/03-selection.md). Order is decided entirely by
// the published_at DESC SQL query in internal/db: there is no importance
// score to rank by any more, so the budget cutoff simply takes the first N
// candidates in that order.
func SelectAndRankCachedTopics(ctx context.Context, database *db.DB, userTags []string, userID string, lengthMinutes int, previousTopics []string) ([]domain.SelectedTopic, int, error) {
	candidates, err := SelectCachedTopics(ctx, database, userTags, userID)
	if err != nil {
		return nil, 0, err
	}
	for index := range candidates {
		candidates[index].IsNew = llm.IsTopicNew(candidates[index].Topic, previousTopics)
	}

	selected := selectTopicsByBudget(candidates, lengthMinutes)
	changeCount := 0
	for index := range selected {
		selected[index].Position = index
		if selected[index].IsNew {
			changeCount++
		}
	}
	return selected, changeCount, nil
}

// selectTopicsByBudget takes the leading candidates (already published_at
// DESC) up to ceil(lengthMinutes*60/assumedChapterSeconds), or all of them if
// fewer are available (backend/docs/generation/03-selection.md step 2).
// assumedChapterSeconds is score.go's constant, shared within this package.
func selectTopicsByBudget(candidates []domain.SelectedTopic, lengthMinutes int) []domain.SelectedTopic {
	if len(candidates) == 0 {
		return nil
	}

	maxChapters := (lengthMinutes*60 + assumedChapterSeconds - 1) / assumedChapterSeconds
	if maxChapters < 1 {
		maxChapters = 1
	}
	if maxChapters > len(candidates) {
		maxChapters = len(candidates)
	}
	return append([]domain.SelectedTopic(nil), candidates[:maxChapters]...)
}

// TopicGroupIDs returns the selected IDs in order for atomic seen-topic
// recording alongside program persistence.
func TopicGroupIDs(topics []domain.SelectedTopic) []string {
	ids := make([]string, 0, len(topics))
	for _, topic := range topics {
		if topic.TopicGroupID != "" {
			ids = append(ids, topic.TopicGroupID)
		}
	}
	return ids
}

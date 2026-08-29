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

// SelectCachedTopics gets fresh article groups from the local cache. An empty
// userID is used by the non-persistent demo endpoint and does not apply the
// seen-topic exclusion.
func SelectCachedTopics(ctx context.Context, database *db.DB, userTags []string, userID string) ([]domain.ScoredTopic, error) {
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

// SelectAndRankCachedTopics selects cached topics, ranks them by their
// ingestion-time importance score, and applies the program-length budget
// without calling an LLM.
func SelectAndRankCachedTopics(ctx context.Context, database *db.DB, userTags []string, userID string, lengthMinutes int, previousTopics []string) ([]domain.ScoredTopic, int, error) {
	candidates, err := SelectCachedTopics(ctx, database, userTags, userID)
	if err != nil {
		return nil, 0, err
	}
	for index := range candidates {
		candidates[index].IsNew = llm.IsTopicNew(candidates[index].Topic, previousTopics)
	}
	selected, changeCount := rankAndSelectTopics(candidates, lengthMinutes)
	return selected, changeCount, nil
}

// TopicGroupIDs returns the selected IDs in order for atomic seen-topic
// recording alongside program persistence.
func TopicGroupIDs(topics []domain.ScoredTopic) []string {
	ids := make([]string, 0, len(topics))
	for _, topic := range topics {
		if topic.TopicGroupID != "" {
			ids = append(ids, topic.TopicGroupID)
		}
	}
	return ids
}

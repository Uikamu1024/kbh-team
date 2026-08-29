package pipeline

import (
	"context"
	"errors"
	"fmt"

	"backend/internal/db"
	"backend/internal/domain"
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
func SelectCachedTopics(ctx context.Context, database *db.DB, userTags []string, userID string) ([]domain.Topic, error) {
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

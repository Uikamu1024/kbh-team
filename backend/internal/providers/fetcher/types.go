package fetcher

import (
	"context"

	"backend/internal/domain"
)

// Fetcher collects and normalizes articles for the requested tags.
type Fetcher interface {
	FetchArticles(ctx context.Context, tags []string) ([]domain.Article, error)
}

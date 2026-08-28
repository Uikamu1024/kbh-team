package pipeline

import (
	"context"
	"fmt"
	"strings"

	"backend/internal/domain"
	"backend/internal/providers/fetcher"
)

// FetchArticles orchestrates article collection for each requested tag.
func FetchArticles(ctx context.Context, f fetcher.Fetcher, tags []string) ([]domain.Article, error) {
	if f == nil {
		return nil, fmt.Errorf("fetcher is nil")
	}

	articles := make([]domain.Article, 0)
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		fetched, err := f.FetchArticles(ctx, []string{tag})
		if err != nil {
			return nil, fmt.Errorf("fetch articles for tag %q: %w", tag, err)
		}
		articles = append(articles, fetched...)
	}
	return articles, nil
}

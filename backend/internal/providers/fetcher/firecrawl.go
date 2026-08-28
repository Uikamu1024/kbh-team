package fetcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"backend/internal/domain"
)

const firecrawlScrapeURL = "https://api.firecrawl.dev/v1/scrape"

// FirecrawlFetcher retrieves article text through the Firecrawl scrape API.
type FirecrawlFetcher struct {
	client *http.Client
}

// NewFirecrawlFetcher creates a Firecrawl-backed Fetcher. A nil client uses
// the default HTTP client.
func NewFirecrawlFetcher(client *http.Client) *FirecrawlFetcher {
	if client == nil {
		client = http.DefaultClient
	}
	return &FirecrawlFetcher{client: client}
}

// FetchArticles fetches the whitelisted articles for each requested tag.
// When FIRECRAWL_API_KEY is unset, it returns deterministic local mock articles.
func (f *FirecrawlFetcher) FetchArticles(ctx context.Context, tags []string) ([]domain.Article, error) {
	if strings.TrimSpace(os.Getenv("FIRECRAWL_API_KEY")) == "" {
		return mockArticles(tags, "firecrawl"), nil
	}

	articles := make([]domain.Article, 0)
	for _, tag := range tags {
		for _, sourceURL := range limitedURLs(whitelistURLs(tag)) {
			article, err := f.fetchArticle(ctx, sourceURL)
			if err != nil {
				return nil, fmt.Errorf("fetch article %q for tag %q: %w", sourceURL, tag, err)
			}
			articles = append(articles, article)
		}
	}
	return articles, nil
}

type firecrawlResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Markdown string            `json:"markdown"`
		Metadata firecrawlMetadata `json:"metadata"`
	} `json:"data"`
}

type firecrawlMetadata struct {
	Title         string `json:"title"`
	PublishedAt   string `json:"publishedAt"`
	PublishedTime string `json:"publishedTime"`
	SourceName    string `json:"sourceName"`
}

func (f *FirecrawlFetcher) fetchArticle(ctx context.Context, sourceURL string) (domain.Article, error) {
	payload, err := json.Marshal(struct {
		URL     string   `json:"url"`
		Formats []string `json:"formats"`
	}{
		URL:     sourceURL,
		Formats: []string{"markdown"},
	})
	if err != nil {
		return domain.Article{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, firecrawlScrapeURL, bytes.NewReader(payload))
	if err != nil {
		return domain.Article{}, err
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(os.Getenv("FIRECRAWL_API_KEY")))
	request.Header.Set("Content-Type", "application/json")

	client := f.client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return domain.Article{}, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return domain.Article{}, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return domain.Article{}, fmt.Errorf("firecrawl returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var result firecrawlResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return domain.Article{}, fmt.Errorf("decode firecrawl response: %w", err)
	}
	if !result.Success {
		if result.Error == "" {
			result.Error = "request was not successful"
		}
		return domain.Article{}, fmt.Errorf("firecrawl request failed: %s", result.Error)
	}

	article := articleFromContent(result.Data.Markdown, sourceURL)
	if result.Data.Metadata.Title != "" {
		article.Title = result.Data.Metadata.Title
	}
	if result.Data.Metadata.SourceName != "" {
		article.SourceName = result.Data.Metadata.SourceName
	}
	if publishedAt := result.Data.Metadata.PublishedAt; publishedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, publishedAt); err == nil {
			article.PublishedAt = parsed
		}
	} else if publishedTime := result.Data.Metadata.PublishedTime; publishedTime != "" {
		if parsed, err := time.Parse(time.RFC3339, publishedTime); err == nil {
			article.PublishedAt = parsed
		}
	}
	return article, nil
}

var _ Fetcher = (*FirecrawlFetcher)(nil)

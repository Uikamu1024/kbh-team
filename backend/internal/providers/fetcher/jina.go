package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"backend/internal/domain"
)

const jinaReaderURL = "https://r.jina.ai/"

// testWhitelist is a temporary whitelist for local development. Replace it
// with the agreed source list when the target sites are decided.
var testWhitelist = map[string][]string{
	"ai": {
		"https://example.com/ai/article-1",
		"https://example.com/ai/article-2",
		"https://example.com/ai/article-3",
	},
	"technology": {
		"https://example.com/technology/article-1",
		"https://example.com/technology/article-2",
		"https://example.com/technology/article-3",
	},
	"テクノロジー": {
		"https://example.com/technology/article-1",
		"https://example.com/technology/article-2",
		"https://example.com/technology/article-3",
	},
	"ニュース": {
		"https://example.com/news/article-1",
		"https://example.com/news/article-2",
		"https://example.com/news/article-3",
	},
}

// JinaFetcher retrieves article text through the jina.ai Reader API.
type JinaFetcher struct {
	client *http.Client
}

// NewJinaFetcher creates a Jina Reader-backed Fetcher. A nil client uses the
// default HTTP client.
func NewJinaFetcher(client *http.Client) *JinaFetcher {
	if client == nil {
		client = http.DefaultClient
	}
	return &JinaFetcher{client: client}
}

// FetchArticles fetches the whitelisted articles for each requested tag.
// When JINA_AI_API_KEY is unset, it returns deterministic local mock articles.
func (f *JinaFetcher) FetchArticles(ctx context.Context, tags []string) ([]domain.Article, error) {
	if strings.TrimSpace(os.Getenv("JINA_AI_API_KEY")) == "" {
		return mockArticles(tags, "jina"), nil
	}

	articles := make([]domain.Article, 0)
	for _, tag := range tags {
		for _, sourceURL := range limitedURLs(whitelistURLs(tag)) {
			article, err := f.FetchArticle(ctx, sourceURL)
			if err != nil {
				return nil, fmt.Errorf("fetch article %q for tag %q: %w", sourceURL, tag, err)
			}
			articles = append(articles, article)
		}
	}
	return articles, nil
}

// FetchArticle retrieves and normalizes the body for one article URL through
// jina.ai Reader. Callers that already have RSS metadata should replace the
// returned title, publication time, and source name with that metadata.
func (f *JinaFetcher) FetchArticle(ctx context.Context, sourceURL string) (domain.Article, error) {
	if f.MockMode() {
		return articleFromContent("# [MOCK] article\n\nThis is a local mock article body.", sourceURL), nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, jinaReaderURL+sourceURL, nil)
	if err != nil {
		return domain.Article{}, err
	}
	request.Header.Set("Accept", "text/markdown")
	if apiKey := strings.TrimSpace(os.Getenv("JINA_AI_API_KEY")); apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := f.client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return domain.Article{}, err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return domain.Article{}, fmt.Errorf("jina reader returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return domain.Article{}, err
	}

	return articleFromContent(string(body), sourceURL), nil
}

// MockMode reports whether Jina credentials are absent and local mock content
// is therefore used instead of a Reader request.
func (f *JinaFetcher) MockMode() bool {
	return strings.TrimSpace(os.Getenv("JINA_AI_API_KEY")) == ""
}

func articleFromContent(content, sourceURL string) domain.Article {
	parsedURL, _ := url.Parse(sourceURL)
	sourceName := parsedURL.Hostname()
	if sourceName == "" {
		sourceName = "unknown"
	}

	return domain.Article{
		Title:       titleFromContent(content, sourceURL),
		Body:        strings.TrimSpace(content),
		PublishedAt: time.Now().UTC(),
		SourceName:  sourceName,
		SourceURL:   sourceURL,
	}
}

func titleFromContent(content, sourceURL string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	if parsedURL, err := url.Parse(sourceURL); err == nil && parsedURL.Path != "" {
		path := strings.Trim(parsedURL.Path, "/")
		if path != "" {
			parts := strings.Split(path, "/")
			return parts[len(parts)-1]
		}
	}
	return sourceURL
}

func whitelistURLs(tag string) []string {
	return testWhitelist[strings.ToLower(strings.TrimSpace(tag))]
}

func limitedURLs(sourceURLs []string) []string {
	const maxArticlesPerTag = 20
	if len(sourceURLs) > maxArticlesPerTag {
		return sourceURLs[:maxArticlesPerTag]
	}
	return sourceURLs
}

func mockArticles(tags []string, provider string) []domain.Article {
	const mockArticlesPerTag = 2
	articles := make([]domain.Article, 0, len(tags)*mockArticlesPerTag)
	now := time.Now().UTC()
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		for index := 1; index <= mockArticlesPerTag; index++ {
			articles = append(articles, domain.Article{
				Title:       fmt.Sprintf("[MOCK] %s topic %d (%s)", tag, index, provider),
				Body:        fmt.Sprintf("[MOCK] This is sample article %d for the %s tag.", index, tag),
				PublishedAt: now.Add(-time.Duration(index) * time.Hour),
				SourceName:  fmt.Sprintf("%s-mock", provider),
				SourceURL:   fmt.Sprintf("https://example.com/mock/%s/%d", url.PathEscape(tag), index),
			})
		}
	}
	return articles
}

var _ Fetcher = (*JinaFetcher)(nil)

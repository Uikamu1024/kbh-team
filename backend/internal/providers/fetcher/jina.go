package fetcher

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"backend/internal/domain"
)

const jinaReaderURL = "https://r.jina.ai/"

// maxArticlesPerTag caps how many articles are fetched per tag per
// generation. jina.ai Reader allows unauthenticated requests at a lower rate
// limit (see https://jina.ai/reader/), and each article also costs one LLM
// scoring call downstream; keeping this modest avoids hitting rate limits
// and keeps total generation time reasonable when a user selects up to 3
// tags in one run (3 tags x 5 articles was measured to occasionally exceed
// a minute and trip free-tier LLM rate limits mid-run).
const maxArticlesPerTag = 3

// tagFeeds maps preset tags (see frontend/src/lib/presetTags.ts) to a public
// RSS feed to source real articles from, replacing the previous placeholder
// example.com whitelist. Coverage is best-effort: some tags share a feed
// because a closer match isn't publicly available (e.g. 京都/旅行 both fall
// back to the general "local" feed). Adjust here if better sources are found.
var tagFeeds = map[string]string{
	"ai":   "https://news.yahoo.co.jp/rss/topics/it.xml",
	"京都":   "https://news.yahoo.co.jp/rss/topics/local.xml",
	"ゲーム":  "https://news.yahoo.co.jp/rss/topics/it.xml",
	"音楽":   "https://news.yahoo.co.jp/rss/topics/entertainment.xml",
	"スポーツ": "https://news.yahoo.co.jp/rss/topics/sports.xml",
	"ビジネス": "https://news.yahoo.co.jp/rss/topics/business.xml",
	"映画":   "https://news.yahoo.co.jp/rss/topics/entertainment.xml",
	"旅行":   "https://news.yahoo.co.jp/rss/topics/local.xml",
}

// defaultFeed is used for tags with no specific mapping above, so an
// unrecognized tag still yields real articles instead of nothing.
const defaultFeed = "https://news.yahoo.co.jp/rss/topics/domestic.xml"

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

// FetchArticles discovers real article URLs for each requested tag via its
// mapped RSS feed (tagFeeds), then fetches each one through jina.ai Reader.
// JINA_AI_API_KEY is optional (see fetchArticle); real fetches are attempted
// either way. A single article failing to fetch (bot-blocked, removed, etc.)
// is skipped rather than aborting the whole batch, since that's common
// enough with real websites that it shouldn't take down generation for every
// other article that did succeed.
func (f *JinaFetcher) FetchArticles(ctx context.Context, tags []string) ([]domain.Article, error) {
	articles := make([]domain.Article, 0)
	for _, tag := range tags {
		sourceURLs, err := fetchFeedURLs(ctx, f.client, feedURLForTag(tag), maxArticlesPerTag)
		if err != nil {
			return nil, fmt.Errorf("fetch RSS feed for tag %q: %w", tag, err)
		}

		for _, sourceURL := range sourceURLs {
			article, err := f.fetchArticle(ctx, sourceURL)
			if err != nil {
				log.Printf("fetcher: skipping article %q for tag %q: %v", sourceURL, tag, err)
				continue
			}
			articles = append(articles, article)
		}
	}
	return articles, nil
}

// feedURLForTag returns the RSS feed mapped to tag, or defaultFeed if none
// is mapped.
func feedURLForTag(tag string) string {
	if feedURL, ok := tagFeeds[strings.ToLower(strings.TrimSpace(tag))]; ok {
		return feedURL
	}
	return defaultFeed
}

// rssFeed is the minimal RSS 2.0 shape needed to pull article links out of a
// feed (see https://www.rssboard.org/rss-specification).
type rssFeed struct {
	Channel struct {
		Items []struct {
			Link string `xml:"link"`
		} `xml:"item"`
	} `xml:"channel"`
}

// fetchFeedURLs fetches feedURL directly (RSS is plain XML, so this doesn't
// go through jina.ai Reader or Firecrawl) and returns up to limit article
// link URLs. Shared by both fetcher providers.
func fetchFeedURLs(ctx context.Context, client *http.Client, feedURL string, limit int) ([]string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("rss feed returned status %d", response.StatusCode)
	}

	var feed rssFeed
	if err := xml.NewDecoder(response.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("parse rss feed: %w", err)
	}

	urls := make([]string, 0, limit)
	for _, item := range feed.Channel.Items {
		link := strings.TrimSpace(item.Link)
		if link == "" {
			continue
		}
		urls = append(urls, link)
		if len(urls) >= limit {
			break
		}
	}
	return urls, nil
}

func (f *JinaFetcher) fetchArticle(ctx context.Context, sourceURL string) (domain.Article, error) {
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

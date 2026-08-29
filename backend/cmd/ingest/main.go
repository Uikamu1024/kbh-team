package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"backend/internal/db"
	"backend/internal/domain"
	"backend/internal/envfile"
	"backend/internal/feedconfig"
	"backend/internal/ingest"
	"backend/internal/providers/fetcher"
	"backend/internal/providers/llm"
)

const (
	feedConfigPath   = "config/article.json"
	maxItemsPerFeed  = 20
	freshnessWindow  = 14 * 24 * time.Hour
	minimumBodyRunes = 100
)

type feedStats struct {
	rssItems      int
	newItems      int
	bodySucceeded int
	bodyFailed    int
}

// collectedArticle is one newly-fetched, metadata-extracted article awaiting
// duplicate resolution and storage (backend/docs/generation/02-ingestion.md
// steps 4-5 done; steps 6-8 still pending, done once for the whole run in
// resolveAndStore).
type collectedArticle struct {
	feedID  string
	article domain.Article
}

func main() {
	log.SetPrefix("ingest: ")
	if err := run(); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}
}

func run() error {
	startedAt := time.Now()
	if err := envfile.Load(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}

	config, err := feedconfig.Load(feedConfigPath)
	if err != nil {
		return err
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}

	openContext, cancelOpen := context.WithTimeout(context.Background(), 30*time.Second)
	database, err := db.Open(openContext, databaseURL)
	cancelOpen()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	lockContext, cancelLock := context.WithTimeout(context.Background(), 10*time.Second)
	lock, acquired, err := database.TryAcquireIngestLock(lockContext)
	cancelLock()
	if err != nil {
		return err
	}
	if !acquired {
		log.Printf("another ingestion run is already active; exiting")
		return nil
	}
	defer lock.Release()

	articleFetcher := fetcher.NewJinaFetcher(&http.Client{Timeout: 30 * time.Second})
	languageModel := llm.FromEnv(&http.Client{Timeout: 60 * time.Second})
	if articleFetcher.MockMode() {
		log.Printf("JINA_AI_API_KEY is not set; mock article bodies will not be cached")
	}
	rssClient := &http.Client{Timeout: 30 * time.Second}

	ctx := context.Background()
	collected := make([]collectedArticle, 0)
	for _, feed := range config.EnabledFeeds() {
		feedArticles, stats := ingestFeed(ctx, database, rssClient, articleFetcher, languageModel, feed)
		collected = append(collected, feedArticles...)
		log.Printf("feed=%s rss=%d new=%d body_success=%d body_failed=%d", feed.ID, stats.rssItems, stats.newItems, stats.bodySucceeded, stats.bodyFailed)
	}

	if err := resolveAndStore(ctx, database, languageModel, collected); err != nil {
		return fmt.Errorf("resolve and store collected articles: %w", err)
	}

	log.Printf("completed in %s: collected=%d", time.Since(startedAt).Round(time.Millisecond), len(collected))
	return nil
}

// ingestFeed fetches one feed's fresh, new RSS items, retrieves each body,
// and extracts LLM metadata for it (backend/docs/generation/02-ingestion.md
// steps 2-5). Duplicate detection and storage are not done here: they happen
// once for the whole run, across every feed, in resolveAndStore (step 6
// requires a single batched LLM call per run, not per feed or per article).
func ingestFeed(ctx context.Context, database *db.DB, rssClient *http.Client, articleFetcher *fetcher.JinaFetcher, languageModel llm.LLM, feed feedconfig.Feed) ([]collectedArticle, feedStats) {
	stats := feedStats{}
	collected := make([]collectedArticle, 0)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
	if err != nil {
		log.Printf("feed=%s create RSS request failed: %v", feed.ID, err)
		return collected, stats
	}
	response, err := rssClient.Do(request)
	if err != nil {
		log.Printf("feed=%s fetch RSS failed: %v", feed.ID, err)
		return collected, stats
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		log.Printf("feed=%s fetch RSS returned status %d", feed.ID, response.StatusCode)
		return collected, stats
	}
	items, err := ingest.ParseRSS(response.Body)
	if err != nil {
		log.Printf("feed=%s parse RSS failed: %v", feed.ID, err)
		return collected, stats
	}
	stats.rssItems = len(items)
	if len(items) > maxItemsPerFeed {
		items = items[:maxItemsPerFeed]
	}

	for _, item := range items {
		if strings.TrimSpace(item.Link) == "" {
			log.Printf("feed=%s skip RSS item without link", feed.ID)
			continue
		}
		if item.HasPublishedAt && item.PublishedAt.Before(time.Now().Add(-freshnessWindow)) {
			continue
		}

		exists, err := database.ArticleExists(ctx, item.Link)
		if err != nil {
			log.Printf("feed=%s check existing article %q failed: %v", feed.ID, item.Link, err)
			continue
		}
		if exists {
			// Tags are no longer feed-decided, so a repeat sighting of an
			// already-cached URL has nothing to merge; just skip it
			// (backend/docs/generation/02-ingestion.md step 3).
			continue
		}

		stats.newItems++
		log.Printf("feed=%s fetching body %d/%d: %s", feed.ID, stats.newItems, len(items), item.Link)
		fetchStartedAt := time.Now()
		fetched, err := articleFetcher.FetchArticle(ctx, item.Link)
		if err != nil {
			stats.bodyFailed++
			log.Printf("feed=%s fetch body for %q failed after %s: %v", feed.ID, item.Link, time.Since(fetchStartedAt).Round(time.Millisecond), err)
			continue
		}
		stats.bodySucceeded++
		log.Printf("feed=%s fetched body %d/%d in %s: %s", feed.ID, stats.newItems, len(items), time.Since(fetchStartedAt).Round(time.Millisecond), item.Link)
		if articleFetcher.MockMode() {
			// Mock bodies are never cached (backend/docs/generation/02-ingestion.md step 4).
			continue
		}
		if utf8.RuneCountInString(fetched.Body) < minimumBodyRunes {
			log.Printf("feed=%s skip short body for %q", feed.ID, item.Link)
			continue
		}

		article := fetched
		if item.Title != "" {
			article.Title = item.Title
		}
		if item.HasPublishedAt {
			article.PublishedAt = item.PublishedAt
		}
		article.SourceName = feed.Title
		article.SourceURL = item.Link

		metadata, err := languageModel.ExtractMetadata(ctx, article.Title, article.Body)
		if err != nil {
			log.Printf("feed=%s extract metadata for %q failed: %v", feed.ID, item.Link, err)
			continue
		}
		article.ShortenedTitle = metadata.ShortenedTitle
		article.Author = metadata.Author
		article.AbbreviatedBody = metadata.AbbreviatedBody
		article.Tags = metadata.Tags
		if metadata.PublishedAt != nil {
			// The LLM confirmed a publish date from the body itself; prefer
			// it over the RSS pubDate (backend/docs/generation/02-ingestion.md step 5).
			article.PublishedAt = *metadata.PublishedAt
		}

		collected = append(collected, collectedArticle{feedID: feed.ID, article: article})
	}
	return collected, stats
}

// resolveAndStore runs the batched LLM duplicate-detection call once for
// every article collected across every feed this run
// (backend/docs/generation/02-ingestion.md step 6), then stores each one
// under its resolved topic_group_id (steps 7-8).
func resolveAndStore(ctx context.Context, database *db.DB, languageModel llm.LLM, collected []collectedArticle) error {
	if len(collected) == 0 {
		return nil
	}

	existingGroups, err := database.RecentPrimaryTopicGroups(ctx)
	if err != nil {
		return fmt.Errorf("list existing topic groups: %w", err)
	}

	newTitles := make([]string, len(collected))
	for index, item := range collected {
		newTitles[index] = item.article.Title
	}

	assignments, err := languageModel.DetectDuplicates(ctx, newTitles, existingGroups)
	if err != nil {
		return fmt.Errorf("detect duplicates: %w", err)
	}
	if len(assignments) != len(collected) {
		return fmt.Errorf("duplicate detection returned %d assignments for %d articles", len(assignments), len(collected))
	}

	for index, item := range collected {
		articleID, err := db.NewUUID()
		if err != nil {
			log.Printf("feed=%s generate article ID for %q failed: %v", item.feedID, item.article.SourceURL, err)
			continue
		}
		if err := database.StoreIngestedArticle(ctx, db.CachedArticle{
			ID:           articleID,
			FeedID:       item.feedID,
			TopicGroupID: assignments[index].TopicGroupID,
			Article:      item.article,
		}); err != nil {
			log.Printf("feed=%s cache article %q failed: %v", item.feedID, item.article.SourceURL, err)
		}
	}
	return nil
}

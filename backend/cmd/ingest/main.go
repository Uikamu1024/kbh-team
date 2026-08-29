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
	"backend/internal/pipeline"
	"backend/internal/providers/fetcher"
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
	if articleFetcher.MockMode() {
		log.Printf("JINA_AI_API_KEY is not set; mock article bodies will not be cached")
	}
	rssClient := &http.Client{Timeout: 30 * time.Second}
	for _, feed := range config.EnabledFeeds() {
		stats := ingestFeed(context.Background(), database, rssClient, articleFetcher, feed)
		log.Printf("feed=%s rss=%d new=%d body_success=%d body_failed=%d", feed.ID, stats.rssItems, stats.newItems, stats.bodySucceeded, stats.bodyFailed)
	}
	log.Printf("completed in %s", time.Since(startedAt).Round(time.Millisecond))
	return nil
}

func ingestFeed(ctx context.Context, database *db.DB, rssClient *http.Client, articleFetcher *fetcher.JinaFetcher, feed feedconfig.Feed) feedStats {
	stats := feedStats{}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
	if err != nil {
		log.Printf("feed=%s create RSS request failed: %v", feed.ID, err)
		return stats
	}
	response, err := rssClient.Do(request)
	if err != nil {
		log.Printf("feed=%s fetch RSS failed: %v", feed.ID, err)
		return stats
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		log.Printf("feed=%s fetch RSS returned status %d", feed.ID, response.StatusCode)
		return stats
	}
	items, err := ingest.ParseRSS(response.Body)
	if err != nil {
		log.Printf("feed=%s parse RSS failed: %v", feed.ID, err)
		return stats
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

		existing, exists, err := database.FindArticleBySourceURL(ctx, item.Link)
		if err != nil {
			log.Printf("feed=%s find article %q failed: %v", feed.ID, item.Link, err)
			continue
		}
		if exists {
			if err := database.MergeCachedArticleTags(ctx, existing, feed.Tags); err != nil {
				log.Printf("feed=%s merge tags for %q failed: %v", feed.ID, item.Link, err)
			}
			continue
		}

		stats.newItems++
		fetched, err := articleFetcher.FetchArticle(ctx, item.Link)
		if err != nil {
			stats.bodyFailed++
			log.Printf("feed=%s fetch body for %q failed: %v", feed.ID, item.Link, err)
			continue
		}
		stats.bodySucceeded++
		if articleFetcher.MockMode() {
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
		article.SourceName = feed.SourceName
		article.SourceURL = item.Link

		recentArticles, err := database.RecentCachedArticles(ctx)
		if err != nil {
			log.Printf("feed=%s list duplicate candidates for %q failed: %v", feed.ID, item.Link, err)
			continue
		}
		matchedGroupID := bestMatchingGroupID(article.Title, recentArticles)
		articleID, err := db.NewUUID()
		if err != nil {
			log.Printf("feed=%s generate article ID for %q failed: %v", feed.ID, item.Link, err)
			continue
		}
		if err := database.StoreIngestedArticle(ctx, db.CachedArticle{
			ID:     articleID,
			FeedID: feed.ID,
			Article: domain.Article{
				Title:       article.Title,
				Body:        article.Body,
				PublishedAt: article.PublishedAt,
				SourceName:  article.SourceName,
				SourceURL:   article.SourceURL,
			},
			Tags: feed.Tags,
		}, matchedGroupID); err != nil {
			log.Printf("feed=%s cache article %q failed: %v", feed.ID, item.Link, err)
		}
	}
	return stats
}

func bestMatchingGroupID(title string, articles []db.CachedArticle) string {
	bestSimilarity := 0.0
	bestGroupID := ""
	for _, article := range articles {
		similarity := pipeline.TitleSimilarity(title, article.Article.Title)
		if similarity >= pipeline.TitleSimilarityThreshold && similarity > bestSimilarity {
			bestSimilarity = similarity
			bestGroupID = article.TopicGroupID
		}
	}
	return bestGroupID
}

package main

import (
	"context"
	"flag"
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
	"backend/internal/trace"
)

const (
	feedConfigPath   = "config/article.json"
	maxItemsPerFeed  = 20
	freshnessWindow  = 14 * 24 * time.Hour
	minimumBodyRunes = 100
)

var (
	limitFlag = flag.Int("limit", 0, "stop after this many new articles are started across all feeds (0 = unlimited). Use a small number for a quick manual test run instead of a full ingest.")
	traceFlag = flag.Bool("trace", false, "print every outbound HTTP request/response (RSS, jina.ai, LLM) live to stderr, and total time spent per stage, to see what is happening and where time is going")
)

type feedStats struct {
	rssItems      int
	newItems      int
	bodySucceeded int
	bodyFailed    int

	rssFetchTime        time.Duration
	bodyFetchTime       time.Duration
	metadataExtractTime time.Duration
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
	flag.Parse()
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

	// -trace shares one recorder across every outbound HTTP client (RSS,
	// jina.ai, LLM) so the printed trace and its per-request timing show the
	// whole run's outbound activity in one interleaved, chronological log —
	// the most direct way to see which stage a slow or failing run is stuck
	// in. Timeouts still work without http.Client.Timeout because each
	// request already carries a context deadline (see below).
	var recorder *trace.Recorder
	newClient := func(timeout time.Duration) *http.Client {
		if recorder != nil {
			return recorder.Client()
		}
		return &http.Client{Timeout: timeout}
	}
	if *traceFlag {
		recorder = trace.NewRecorder(nil)
		recorder.Log = printTraceEntry
	}
	if *limitFlag > 0 {
		log.Printf("limit=%d: stopping after this many new articles are started", *limitFlag)
	}

	articleFetcher := fetcher.NewJinaFetcher(newClient(30 * time.Second))
	languageModel := llm.FromEnv(newClient(60 * time.Second))
	if articleFetcher.MockMode() {
		log.Printf("JINA_AI_API_KEY is not set; mock article bodies will not be cached")
	}
	rssClient := newClient(30 * time.Second)

	ctx := context.Background()
	collected := make([]collectedArticle, 0)
	var totalRSSFetchTime, totalBodyFetchTime, totalMetadataExtractTime time.Duration
	for _, feed := range config.EnabledFeeds() {
		if *limitFlag > 0 && len(collected) >= *limitFlag {
			log.Printf("limit=%d reached; skipping remaining feeds", *limitFlag)
			break
		}
		remaining := 0
		if *limitFlag > 0 {
			remaining = *limitFlag - len(collected)
		}

		feedArticles, stats := ingestFeed(ctx, database, rssClient, articleFetcher, languageModel, feed, remaining)
		collected = append(collected, feedArticles...)
		totalRSSFetchTime += stats.rssFetchTime
		totalBodyFetchTime += stats.bodyFetchTime
		totalMetadataExtractTime += stats.metadataExtractTime
		log.Printf("feed=%s rss=%d new=%d body_success=%d body_failed=%d rss_fetch=%s body_fetch=%s metadata_extract=%s",
			feed.ID, stats.rssItems, stats.newItems, stats.bodySucceeded, stats.bodyFailed,
			stats.rssFetchTime.Round(time.Millisecond), stats.bodyFetchTime.Round(time.Millisecond), stats.metadataExtractTime.Round(time.Millisecond))
	}

	resolveStartedAt := time.Now()
	if err := resolveAndStore(ctx, database, languageModel, collected); err != nil {
		return fmt.Errorf("resolve and store collected articles: %w", err)
	}

	log.Printf("timing: rss_fetch=%s body_fetch=%s metadata_extract=%s resolve_and_store=%s total=%s",
		totalRSSFetchTime.Round(time.Millisecond), totalBodyFetchTime.Round(time.Millisecond), totalMetadataExtractTime.Round(time.Millisecond),
		time.Since(resolveStartedAt).Round(time.Millisecond), time.Since(startedAt).Round(time.Millisecond))
	log.Printf("completed in %s: collected=%d", time.Since(startedAt).Round(time.Millisecond), len(collected))
	return nil
}

// ingestFeed fetches one feed's fresh, new RSS items, retrieves each body,
// and extracts LLM metadata for it (backend/docs/generation/02-ingestion.md
// steps 2-5). Duplicate detection and storage are not done here: they happen
// once for the whole run, across every feed, in resolveAndStore (step 6
// requires a single batched LLM call per run, not per feed or per article).
// remaining caps how many new items this feed may start (0 = unlimited),
// counting toward the run-wide -limit budget.
func ingestFeed(ctx context.Context, database *db.DB, rssClient *http.Client, articleFetcher *fetcher.JinaFetcher, languageModel llm.LLM, feed feedconfig.Feed, remaining int) ([]collectedArticle, feedStats) {
	stats := feedStats{}
	collected := make([]collectedArticle, 0)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
	if err != nil {
		log.Printf("feed=%s create RSS request failed: %v", feed.ID, err)
		return collected, stats
	}
	rssStartedAt := time.Now()
	response, err := rssClient.Do(request)
	if err != nil {
		stats.rssFetchTime = time.Since(rssStartedAt)
		log.Printf("feed=%s fetch RSS failed after %s: %v", feed.ID, stats.rssFetchTime.Round(time.Millisecond), err)
		return collected, stats
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		stats.rssFetchTime = time.Since(rssStartedAt)
		log.Printf("feed=%s fetch RSS returned status %d after %s", feed.ID, response.StatusCode, stats.rssFetchTime.Round(time.Millisecond))
		return collected, stats
	}
	items, err := ingest.ParseRSS(response.Body)
	stats.rssFetchTime = time.Since(rssStartedAt)
	if err != nil {
		log.Printf("feed=%s parse RSS failed: %v", feed.ID, err)
		return collected, stats
	}
	stats.rssItems = len(items)
	if len(items) > maxItemsPerFeed {
		items = items[:maxItemsPerFeed]
	}

	for _, item := range items {
		if remaining > 0 && stats.newItems >= remaining {
			log.Printf("feed=%s limit reached for this run; skipping remaining RSS items", feed.ID)
			break
		}
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
		fetchDuration := time.Since(fetchStartedAt)
		stats.bodyFetchTime += fetchDuration
		if err != nil {
			stats.bodyFailed++
			log.Printf("feed=%s fetch body for %q failed after %s: %v", feed.ID, item.Link, fetchDuration.Round(time.Millisecond), err)
			continue
		}
		stats.bodySucceeded++
		log.Printf("feed=%s fetched body %d/%d in %s: %s", feed.ID, stats.newItems, len(items), fetchDuration.Round(time.Millisecond), item.Link)
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

		metadataStartedAt := time.Now()
		metadata, err := languageModel.ExtractMetadata(ctx, article.Title, article.Body)
		metadataDuration := time.Since(metadataStartedAt)
		stats.metadataExtractTime += metadataDuration
		if err != nil {
			log.Printf("feed=%s extract metadata for %q failed after %s: %v", feed.ID, item.Link, metadataDuration.Round(time.Millisecond), err)
			continue
		}
		log.Printf("feed=%s extracted metadata %d/%d in %s: %s (tags=%v)", feed.ID, stats.newItems, len(items), metadataDuration.Round(time.Millisecond), item.Link, metadata.Tags)
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
// under its resolved topic_group_id (steps 7-8). Unlike per-article metadata
// extraction, a failure here (e.g. the duplicate-detection call itself
// erroring) aborts the whole run without storing anything, since there is
// only one such call per run and no partial result to fall back to.
func resolveAndStore(ctx context.Context, database *db.DB, languageModel llm.LLM, collected []collectedArticle) error {
	if len(collected) == 0 {
		log.Printf("no new articles collected this run; nothing to resolve or store")
		return nil
	}

	existingGroupsStartedAt := time.Now()
	existingGroups, err := database.RecentPrimaryTopicGroups(ctx)
	if err != nil {
		return fmt.Errorf("list existing topic groups: %w", err)
	}
	log.Printf("found %d existing primary topic groups (last 14 days) in %s", len(existingGroups), time.Since(existingGroupsStartedAt).Round(time.Millisecond))

	newTitles := make([]string, len(collected))
	for index, item := range collected {
		newTitles[index] = item.article.Title
	}

	log.Printf("resolving duplicates for %d newly collected articles", len(collected))
	duplicateStartedAt := time.Now()
	assignments, err := languageModel.DetectDuplicates(ctx, newTitles, existingGroups)
	duplicateDuration := time.Since(duplicateStartedAt)
	if err != nil {
		return fmt.Errorf("detect duplicates (after %s): %w", duplicateDuration.Round(time.Millisecond), err)
	}
	log.Printf("duplicate detection completed in %s", duplicateDuration.Round(time.Millisecond))
	if len(assignments) != len(collected) {
		return fmt.Errorf("duplicate detection returned %d assignments for %d articles", len(assignments), len(collected))
	}

	storeStartedAt := time.Now()
	stored := 0
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
			continue
		}
		stored++
	}
	log.Printf("stored %d/%d articles in %s", stored, len(collected), time.Since(storeStartedAt).Round(time.Millisecond))
	return nil
}

func printTraceEntry(entry trace.Entry) {
	status := fmt.Sprintf("%d", entry.StatusCode)
	if entry.Error != "" {
		status = "ERROR"
	}
	fmt.Fprintf(os.Stderr, "\n--- [%d] %s %s -> %s (%dms) ---\n", entry.Sequence, entry.Method, entry.URL, status, entry.DurationMs)
	if entry.RequestBody != "" {
		fmt.Fprintf(os.Stderr, "request:  %s\n", entry.RequestBody)
	}
	if entry.Error != "" {
		fmt.Fprintf(os.Stderr, "error: %s\n", entry.Error)
		return
	}
	if entry.ResponseBody != "" {
		fmt.Fprintf(os.Stderr, "response: %s\n", entry.ResponseBody)
	}
}

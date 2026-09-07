// gentags is a standalone developer tool that proposes a new config/tags.json
// taxonomy from the actual content of the feeds in config/rss.json. It is not
// part of the production pipeline (internal/pipeline, internal/api never
// import this package) and it does not touch backend/internal/providers/llm —
// it has its own minimal, self-contained LLM-calling code (see llm.go)
// because this is a one-off exploration tool, not a swappable production
// provider. It never writes to the database and never runs automatically.
//
// Usage: go run ./cmd/gentags [-limit-feeds N] [-items-per-feed N]
// [-chunk-size N] [-min-tags N] [-max-tags N] [-write]
//
// By default it only prints the proposed tags; pass -write to overwrite
// config/tags.json directly. It never touches frontend/src/config/tags.json
// (that copy has to be synced by hand after reviewing the proposal).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"backend/internal/envfile"
	"backend/internal/feedconfig"
	"backend/internal/ingest"
	"backend/internal/providers/fetcher"
)

const (
	feedConfigPath = "../config/rss.json"
	tagsConfigPath = "../config/tags.json"
)

var (
	limitFeedsFlag   = flag.Int("limit-feeds", 0, "only process N enabled feeds (0 = all). Use a small number to try the tool out before running it against every feed. Picks the first N unless -random-feeds is set.")
	randomFeedsFlag  = flag.Bool("random-feeds", false, "with -limit-feeds, pick a random sample of feeds instead of always the first N in config/rss.json's order")
	itemsPerFeedFlag = flag.Int("items-per-feed", 2, "how many of each feed's most recent RSS items to fetch article text for")
	chunkSizeFlag    = flag.Int("chunk-size", 25, "how many feeds' worth of text to send in one map-stage LLM call")
	minTagsFlag      = flag.Int("min-tags", 8, "minimum number of final tags to request from the reduce stage")
	maxTagsFlag      = flag.Int("max-tags", 12, "maximum number of final tags to request from the reduce stage")
	writeFlag        = flag.Bool("write", false, "overwrite config/tags.json with the proposed tags (default: print only)")
)

func main() {
	log.SetPrefix("gentags: ")
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
	if *minTagsFlag < 1 || *maxTagsFlag < *minTagsFlag {
		return fmt.Errorf("invalid -min-tags/-max-tags: %d/%d", *minTagsFlag, *maxTagsFlag)
	}

	config, err := feedconfig.Load(feedConfigPath)
	if err != nil {
		return err
	}
	feeds := config.EnabledFeeds()
	if *limitFeedsFlag > 0 && len(feeds) > *limitFeedsFlag {
		if *randomFeedsFlag {
			rand.Shuffle(len(feeds), func(i, j int) { feeds[i], feeds[j] = feeds[j], feeds[i] })
		}
		feeds = feeds[:*limitFeedsFlag]
	}
	log.Printf("processing %d feeds (items-per-feed=%d, random=%v)", len(feeds), *itemsPerFeedFlag, *randomFeedsFlag)

	ctx := context.Background()
	rssClient := &http.Client{Timeout: 30 * time.Second}
	fetchClient := &http.Client{Timeout: 20 * time.Second}
	llmClient := &http.Client{Timeout: 60 * time.Second}
	jinaFetcher := fetcher.NewJinaFetcher(fetchClient)
	if jinaFetcher.MockMode() {
		log.Printf("JINA_AI_API_KEY is not set; the jina.ai fallback will run unauthenticated at a lower rate limit")
	}

	corpora := make([]string, 0, len(feeds))
	for index, feed := range feeds {
		corpus := collectFeedCorpus(ctx, rssClient, jinaFetcher, feed)
		if corpus == "" {
			log.Printf("feed=%s (%d/%d): no usable text, skipping", feed.ID, index+1, len(feeds))
			continue
		}
		log.Printf("feed=%s (%d/%d): collected %d chars", feed.ID, index+1, len(feeds), len(corpus))
		corpora = append(corpora, corpus)
	}
	if len(corpora) == 0 {
		return fmt.Errorf("no feed text was collected; nothing to propose tags from")
	}

	candidates, err := mapStage(ctx, llmClient, corpora)
	if err != nil {
		return fmt.Errorf("map stage: %w", err)
	}
	log.Printf("map stage produced %d candidate tags across %d chunks", len(candidates), (len(corpora)+*chunkSizeFlag-1)/(*chunkSizeFlag))

	finalTags, err := reduceStage(ctx, llmClient, candidates)
	if err != nil {
		return fmt.Errorf("reduce stage: %w", err)
	}

	output, err := json.MarshalIndent(finalTags, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(output))
	fmt.Println()
	fmt.Println("上のタグ一覧をレビューしてから config/tags.json に反映してください。")
	fmt.Println("frontend/src/config/tags.json は自動更新されないので、手動で合わせてください。")

	if *writeFlag {
		if err := os.WriteFile(tagsConfigPath, append(output, '\n'), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", tagsConfigPath, err)
		}
		log.Printf("wrote %s", tagsConfigPath)
	}

	log.Printf("completed in %s", time.Since(startedAt).Round(time.Millisecond))
	return nil
}

// collectFeedCorpus fetches one feed's RSS, takes its most recent
// itemsPerFeedFlag items, and fetches article text for each (see
// fetchArticleText in fetch.go). A single feed or item failing is logged and
// skipped rather than aborting the run.
func collectFeedCorpus(ctx context.Context, rssClient *http.Client, jinaFetcher *fetcher.JinaFetcher, feed feedconfig.Feed) string {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
	if err != nil {
		log.Printf("feed=%s create RSS request failed: %v", feed.ID, err)
		return ""
	}
	response, err := rssClient.Do(request)
	if err != nil {
		log.Printf("feed=%s fetch RSS failed: %v", feed.ID, err)
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		log.Printf("feed=%s fetch RSS returned status %d", feed.ID, response.StatusCode)
		return ""
	}

	items, err := ingest.ParseRSS(response.Body)
	if err != nil {
		log.Printf("feed=%s parse RSS failed: %v", feed.ID, err)
		return ""
	}
	if len(items) > *itemsPerFeedFlag {
		items = items[:*itemsPerFeedFlag]
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "### %s\n", feed.Title)
	for _, item := range items {
		if strings.TrimSpace(item.Link) == "" {
			continue
		}
		text, err := fetchArticleText(ctx, rssClient, jinaFetcher, item.Link)
		if err != nil {
			log.Printf("feed=%s fetch article text for %q failed: %v", feed.ID, item.Link, err)
			continue
		}
		snippet := text
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		fmt.Fprintf(&builder, "- %s: %s\n", item.Title, snippet)
	}
	return builder.String()
}

// mapStage sends corpora in chunks of chunkSizeFlag feeds each, asking the
// LLM for candidate tags per chunk, and returns the union of all candidates
// (exact-match deduplication only — the reduce stage is responsible for
// merging near-duplicates and pruning to the final count).
func mapStage(ctx context.Context, client *http.Client, corpora []string) ([]string, error) {
	seen := make(map[string]bool)
	candidates := make([]string, 0)

	chunkSize := *chunkSizeFlag
	for start := 0; start < len(corpora); start += chunkSize {
		end := start + chunkSize
		if end > len(corpora) {
			end = len(corpora)
		}
		chunk := corpora[start:end]

		var builder strings.Builder
		for _, corpus := range chunk {
			builder.WriteString(corpus)
			builder.WriteString("\n")
		}

		prompt := fmt.Sprintf(`以下は複数のニュース・ブログRSSフィードのタイトルと記事本文の抜粋です。
これらの記事を分類するのに使えそうな、幅広いテーマのタグ候補を日本語の短い単語（2〜8文字程度、既存の"AI"や"京都"のような名詞）で5〜15個挙げてください。
細かすぎる粒度（個別の企業名・製品名など）は避け、複数の記事に共通して使えるカテゴリ名にしてください。

%s`, builder.String())

		tags, err := proposeTags(ctx, client, prompt, 5, 15)
		if err != nil {
			return nil, fmt.Errorf("chunk %d-%d: %w", start, end, err)
		}
		for _, tag := range tags {
			if !seen[tag] {
				seen[tag] = true
				candidates = append(candidates, tag)
			}
		}
	}
	return candidates, nil
}

// reduceStage consolidates every map-stage candidate tag into the final
// minTagsFlag..maxTagsFlag list, merging synonyms and near-duplicates. This
// is the step that keeps the taxonomy from fragmenting into too many
// overlapping tags.
func reduceStage(ctx context.Context, client *http.Client, candidates []string) ([]string, error) {
	prompt := fmt.Sprintf(`次のタグ候補一覧は、複数のニュース・ブログ記事の集まりから複数回に分けて提案されたものです。
同義語・意味が近すぎるもの・粒度が細かすぎるものは統合し、幅広く使えるテーマだけを残して、必ず%d〜%d個の最終的なタグ一覧に絞り込んでください。
出力するタグは日本語の短い単語（2〜8文字程度）にしてください。

候補一覧:
%s`, *minTagsFlag, *maxTagsFlag, strings.Join(candidates, "、"))

	return proposeTags(ctx, client, prompt, *minTagsFlag, *maxTagsFlag)
}

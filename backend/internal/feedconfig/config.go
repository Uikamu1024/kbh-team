// Package feedconfig loads the RSS feed definitions used by cmd/ingest.
package feedconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Config is the top-level rss.json document: a flat array of feeds.
type Config []Feed

// Feed describes one RSS source (backend/docs/generation/01-feed-config.md).
// Tags are no longer configured per-feed: the ingest job's LLM classifies
// tags from each article's own content instead
// (backend/docs/generation/02-ingestion.md step 5).
type Feed struct {
	Title   string `json:"title"`
	ID      string `json:"id"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

// Load reads and validates an rss.json feed configuration.
func Load(path string) (Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read feed config: %w", err)
	}

	var config Config
	if err := json.Unmarshal(contents, &config); err != nil {
		return nil, fmt.Errorf("parse feed config: %w", err)
	}
	for index, feed := range config {
		if strings.TrimSpace(feed.ID) == "" {
			return nil, fmt.Errorf("feed %d: id is required", index)
		}
		if strings.TrimSpace(feed.URL) == "" {
			return nil, fmt.Errorf("feed %q: url is required", feed.ID)
		}
		if strings.TrimSpace(feed.Title) == "" {
			return nil, fmt.Errorf("feed %q: title is required", feed.ID)
		}
	}
	return config, nil
}

// EnabledFeeds returns the configured feeds that should be fetched.
func (c Config) EnabledFeeds() []Feed {
	feeds := make([]Feed, 0, len(c))
	for _, feed := range c {
		if feed.Enabled {
			feeds = append(feeds, feed)
		}
	}
	return feeds
}

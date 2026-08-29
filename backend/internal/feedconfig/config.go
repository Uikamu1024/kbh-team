// Package feedconfig loads the RSS feed definitions used by cmd/ingest.
package feedconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Config is the top-level article.json document.
type Config struct {
	Feeds []Feed `json:"feeds"`
}

// Feed describes one RSS source (backend/docs/generation/01-feed-config.md).
// Tags are no longer configured per-feed: the ingest job's LLM classifies
// tags from each article's own content instead
// (backend/docs/generation/02-ingestion.md step 5).
type Feed struct {
	ID      string `json:"id"`
	URL     string `json:"url"`
	Origin  string `json:"origin"`
	Title   string `json:"title"`
	Enabled bool   `json:"enabled"`
}

// Load reads and validates an article.json feed configuration.
func Load(path string) (Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read feed config: %w", err)
	}

	var config Config
	if err := json.Unmarshal(contents, &config); err != nil {
		return Config{}, fmt.Errorf("parse feed config: %w", err)
	}
	for index, feed := range config.Feeds {
		if strings.TrimSpace(feed.ID) == "" {
			return Config{}, fmt.Errorf("feed %d: id is required", index)
		}
		if strings.TrimSpace(feed.URL) == "" {
			return Config{}, fmt.Errorf("feed %q: url is required", feed.ID)
		}
		if strings.TrimSpace(feed.Title) == "" {
			return Config{}, fmt.Errorf("feed %q: title is required", feed.ID)
		}
	}
	return config, nil
}

// EnabledFeeds returns the configured feeds that should be fetched.
func (c Config) EnabledFeeds() []Feed {
	feeds := make([]Feed, 0, len(c.Feeds))
	for _, feed := range c.Feeds {
		if feed.Enabled {
			feeds = append(feeds, feed)
		}
	}
	return feeds
}

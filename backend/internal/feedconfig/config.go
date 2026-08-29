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

// Feed describes one RSS source and the application tags it supplies.
type Feed struct {
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	SourceName string   `json:"sourceName"`
	Tags       []string `json:"tags"`
	Enabled    bool     `json:"enabled"`
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
		if strings.TrimSpace(feed.SourceName) == "" {
			return Config{}, fmt.Errorf("feed %q: sourceName is required", feed.ID)
		}
		if len(feed.Tags) == 0 {
			return Config{}, fmt.Errorf("feed %q: at least one tag is required", feed.ID)
		}
		for tagIndex, tag := range feed.Tags {
			if strings.TrimSpace(tag) == "" {
				return Config{}, fmt.Errorf("feed %q: tag %d is empty", feed.ID, tagIndex)
			}
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

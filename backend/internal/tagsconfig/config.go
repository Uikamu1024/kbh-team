// Package tagsconfig loads the preset article tag list (config/tags.json)
// used by cmd/ingest's LLM metadata extraction (domain.PresetTags).
package tagsconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Config is the top-level tags.json document.
type Config struct {
	Tags []string `json:"tags"`
}

// Load reads and validates a tags.json preset tag list.
func Load(path string) ([]string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tags config: %w", err)
	}

	var config Config
	if err := json.Unmarshal(contents, &config); err != nil {
		return nil, fmt.Errorf("parse tags config: %w", err)
	}
	if len(config.Tags) == 0 {
		return nil, fmt.Errorf("tags config: at least one tag is required")
	}
	for index, tag := range config.Tags {
		if strings.TrimSpace(tag) == "" {
			return nil, fmt.Errorf("tags config: tag %d is empty", index)
		}
	}
	return config.Tags, nil
}

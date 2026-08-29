// Package tagsconfig loads the preset article tag list (config/tags.json)
// used by cmd/ingest's LLM metadata extraction (domain.PresetTags).
package tagsconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Load reads and validates a tags.json preset tag list: a flat JSON array of
// strings.
func Load(path string) ([]string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tags config: %w", err)
	}

	var tags []string
	if err := json.Unmarshal(contents, &tags); err != nil {
		return nil, fmt.Errorf("parse tags config: %w", err)
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("tags config: at least one tag is required")
	}
	for index, tag := range tags {
		if strings.TrimSpace(tag) == "" {
			return nil, fmt.Errorf("tags config: tag %d is empty", index)
		}
	}
	return tags, nil
}

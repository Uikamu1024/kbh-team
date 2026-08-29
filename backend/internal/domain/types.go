// Package domain contains the shared data types used across the pipeline.
package domain

import "time"

// Article is a normalized article collected from a source.
type Article struct {
	Title       string
	Body        string
	PublishedAt time.Time
	SourceName  string
	SourceURL   string

	// ShortenedTitle, Author, AbbreviatedBody, and Tags are populated by the
	// ingest job's LLM metadata extraction (backend/docs/generation/02-ingestion.md
	// step 5) and persisted on the articles table (backend/docs/generation/04-schema.md).
	// They are empty for articles produced by the live-fetch cmd/demo path, which
	// does not run metadata extraction.
	ShortenedTitle  string
	Author          string
	AbbreviatedBody string
	Tags            []string
}

// Topic groups articles considered to cover the same story.
type Topic struct {
	Primary      Article
	RelatedCount int
	TopicGroupID string
}

// ScoredTopic is a topic annotated with its LLM importance score and program
// position. Used only by the live-fetch cmd/demo path
// (backend/docs/pipeline/03-score.md); the cache-backed selection path
// (backend/docs/generation/03-selection.md) has no importance score and uses
// SelectedTopic instead.
type ScoredTopic struct {
	Topic
	ImportanceScore int
	IsNew           bool
	Position        int
}

// SelectedTopic is a cached topic chosen for a program via
// backend/docs/generation/03-selection.md's tag+freshness selection. Unlike
// ScoredTopic, it carries no importance score: selection order is published_at
// DESC only, decided by the SQL query in internal/db, not by Go-side ranking.
type SelectedTopic struct {
	Topic
	IsNew    bool
	Position int
}

// ChapterDraft is the script for one selected topic.
type ChapterDraft struct {
	ScoredTopic
	Lines []Line
}

// Line is one spoken line in a chapter.
type Line struct {
	Speaker string
	Text    string
}

// ChapterAudio is a chapter draft with its synthesized WAV audio.
type ChapterAudio struct {
	ChapterDraft
	AudioBytes  []byte
	DurationSec int
}

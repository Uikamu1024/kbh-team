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
}

// Topic groups articles considered to cover the same story.
type Topic struct {
	Primary      Article
	RelatedCount int
}

// ScoredTopic is a topic annotated with its importance and program position.
type ScoredTopic struct {
	Topic
	ImportanceScore int
	IsNew           bool
	Position        int
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

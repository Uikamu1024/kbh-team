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

// PresetTags is the fixed set of theme tags an article may be classified
// into by the ingest job's LLM metadata extraction
// (backend/docs/generation/01-feed-config.md: "フロントエンドの固定タグ一覧と
// 1文字違わず一致する値に限定する"). PROVISIONAL: mirrors the four tags that
// were previously hardcoded per-feed in config/article.json before the
// 2026-08-29 tagging redesign, used as a stand-in until the frontend owner
// confirms the authoritative onboarding preset list
// (docs/api-contract.yaml's open item, "プリセットタグ一覧の管理場所").
var PresetTags = []string{"AI", "テクノロジー", "ゲーム", "京都"}

// ArticleMetadata is the result of LLM structured-output metadata extraction
// for one article (backend/docs/generation/02-ingestion.md step 5).
type ArticleMetadata struct {
	ShortenedTitle  string     // UI表示用の短いタイトル、45文字以内
	Tags            []string   // PresetTagsの部分集合
	Author          string     // 本文から抽出できなければ""
	PublishedAt     *time.Time // 本文から確認できなければnil（RSSのpubDateを使う）
	AbbreviatedBody string     // 元本文が2500文字を超える場合のみ非空（要約）
}

// ExistingTopicGroup is one candidate existing group offered to the batched
// duplicate-detection LLM call (backend/docs/generation/02-ingestion.md step
// 6): a recent, primary article's original title and its topic_group_id.
type ExistingTopicGroup struct {
	TopicGroupID string
	Title        string
}

// DuplicateAssignment maps one newly-ingested article (by its original
// title) to the topic_group_id it should be saved under, as decided by the
// batched LLM duplicate-detection call
// (backend/docs/generation/02-ingestion.md step 6). TopicGroupID may be an
// ExistingTopicGroup's ID (joined an existing group), a freshly minted ID
// shared with other entries in the same ingest batch (grouped with each
// other), or a freshly minted ID unique to this article (no match found).
type DuplicateAssignment struct {
	Title        string
	TopicGroupID string
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

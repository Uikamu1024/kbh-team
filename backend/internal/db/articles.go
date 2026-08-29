package db

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const ingestAdvisoryLockID int64 = 734_128_091

// CachedArticle is an articles-table row used during ingestion.
// TopicGroupID must already be resolved by the caller (backend/docs/generation/02-ingestion.md
// step 6, LLM-based duplicate detection) before calling StoreIngestedArticle;
// unlike the previous bigram-similarity design, this package no longer picks
// topic_group_id itself.
type CachedArticle struct {
	ID           string
	FeedID       string
	TopicGroupID string
	Article      domain.Article
}

// IngestLock holds the dedicated PostgreSQL connection for an ingestion run.
// Advisory locks are connection-scoped, so it must remain open until the job
// completes.
type IngestLock struct {
	connection *pgxpool.Conn
}

// TryAcquireIngestLock prevents overlapping cmd/ingest executions.
func (d *DB) TryAcquireIngestLock(ctx context.Context) (*IngestLock, bool, error) {
	connection, err := d.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire advisory lock connection: %w", err)
	}
	var acquired bool
	if err := connection.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, ingestAdvisoryLockID).Scan(&acquired); err != nil {
		connection.Release()
		return nil, false, fmt.Errorf("try ingestion advisory lock: %w", err)
	}
	if !acquired {
		connection.Release()
		return nil, false, nil
	}
	return &IngestLock{connection: connection}, true, nil
}

// Release unlocks and returns the connection to the pool.
func (l *IngestLock) Release() {
	if l == nil || l.connection == nil {
		return
	}
	_, _ = l.connection.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, ingestAdvisoryLockID)
	l.connection.Release()
	l.connection = nil
}

// ArticleExists reports whether an article with this exact source URL is
// already cached. On a match, backend/docs/generation/02-ingestion.md step 3
// says to simply skip the URL (tags are no longer feed-decided, so there is
// no tag-merge step on a repeat sighting).
func (d *DB) ArticleExists(ctx context.Context, sourceURL string) (bool, error) {
	var exists bool
	if err := d.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM articles WHERE source_url = $1)`, sourceURL).Scan(&exists); err != nil {
		return false, fmt.Errorf("check existing article: %w", err)
	}
	return exists, nil
}

// RecentPrimaryTopicGroups returns the title and topic_group_id of every
// primary article published within the last 14 days, the candidate list
// offered to the batched LLM duplicate-detection call
// (backend/docs/generation/02-ingestion.md step 6).
func (d *DB) RecentPrimaryTopicGroups(ctx context.Context) ([]domain.ExistingTopicGroup, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT topic_group_id::text, title
		FROM articles
		WHERE is_primary = true
		  AND published_at >= now() - interval '14 days'`)
	if err != nil {
		return nil, fmt.Errorf("list recent primary topic groups: %w", err)
	}
	defer rows.Close()

	groups := make([]domain.ExistingTopicGroup, 0)
	for rows.Next() {
		var group domain.ExistingTopicGroup
		if err := rows.Scan(&group.TopicGroupID, &group.Title); err != nil {
			return nil, fmt.Errorf("scan recent primary topic group: %w", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent primary topic groups: %w", err)
	}
	return groups, nil
}

// StoreIngestedArticle saves a new article under article.TopicGroupID
// (already resolved by the caller via LLM duplicate detection) and updates
// that group's primary article and tag union atomically
// (backend/docs/generation/02-ingestion.md steps 7-8). Whether this article
// becomes the group's primary is decided here: if no primary exists yet for
// this topic_group_id (a brand-new group, or the first of a batch of new
// articles the LLM grouped together), this article becomes primary; if a
// primary already exists, this article becomes primary only if its body is
// longer.
func (d *DB) StoreIngestedArticle(ctx context.Context, article CachedArticle) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin article insert: %w", err)
	}
	defer tx.Rollback(ctx)

	var existingPrimaryID, existingPrimaryBody string
	err = tx.QueryRow(ctx, `
		SELECT id::text, body FROM articles
		WHERE topic_group_id = $1::uuid AND is_primary = true
		FOR UPDATE`, article.TopicGroupID).Scan(&existingPrimaryID, &existingPrimaryBody)
	hasExistingPrimary := true
	if err == pgx.ErrNoRows {
		hasExistingPrimary = false
	} else if err != nil {
		return fmt.Errorf("find group primary: %w", err)
	}
	primary := !hasExistingPrimary

	if _, err := tx.Exec(ctx, `
		INSERT INTO articles (
			id, feed_id, topic_group_id, is_primary, title, shortened_title, author,
			body, abbreviated_body, published_at, source_name, source_url, tags
		) VALUES (
			$1::uuid, $2, $3::uuid, $4, $5, NULLIF($6, ''), NULLIF($7, ''),
			$8, NULLIF($9, ''), $10, $11, $12, $13::text[]
		)`, article.ID, article.FeedID, article.TopicGroupID, primary, article.Article.Title,
		article.Article.ShortenedTitle, article.Article.Author, article.Article.Body,
		article.Article.AbbreviatedBody, article.Article.PublishedAt, article.Article.SourceName,
		article.Article.SourceURL, article.Article.Tags); err != nil {
		return fmt.Errorf("insert cached article: %w", err)
	}

	if hasExistingPrimary && utf8.RuneCountInString(article.Article.Body) > utf8.RuneCountInString(existingPrimaryBody) {
		if _, err := tx.Exec(ctx, `UPDATE articles SET is_primary = false WHERE id = $1::uuid`, existingPrimaryID); err != nil {
			return fmt.Errorf("clear previous group primary: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE articles SET is_primary = true WHERE id = $1::uuid`, article.ID); err != nil {
			return fmt.Errorf("set replacement group primary: %w", err)
		}
	}

	rows, err := tx.Query(ctx, `SELECT tags FROM articles WHERE topic_group_id = $1::uuid`, article.TopicGroupID)
	if err != nil {
		return fmt.Errorf("read group tags: %w", err)
	}
	allTags := make([]string, 0)
	for rows.Next() {
		var tags []string
		if err := rows.Scan(&tags); err != nil {
			rows.Close()
			return fmt.Errorf("scan group tags: %w", err)
		}
		allTags = unionTags(allTags, tags)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate group tags: %w", err)
	}
	rows.Close()
	if _, err := tx.Exec(ctx, `
		UPDATE articles SET tags = $2::text[]
		WHERE topic_group_id = $1::uuid AND is_primary = true`, article.TopicGroupID, allTags); err != nil {
		return fmt.Errorf("update group primary tags: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cached article: %w", err)
	}
	return nil
}

// SelectCachedTopics returns primary article groups matching user tags,
// ordered by published_at DESC (backend/docs/generation/03-selection.md —
// there is no importance score to rank by; publish freshness is the only
// order). When excludeSeen is true, already delivered topic groups are
// omitted.
func (d *DB) SelectCachedTopics(ctx context.Context, tags []string, userID string, excludeSeen bool) ([]domain.SelectedTopic, error) {
	query := `
		SELECT a.topic_group_id::text, a.title, a.body, a.published_at,
		       a.source_name, a.source_url, a.tags,
		       COALESCE(a.shortened_title, ''), COALESCE(a.author, ''),
		       COALESCE(a.abbreviated_body, ''), g.related_count
		FROM articles a
		JOIN (
			SELECT topic_group_id, count(*) AS related_count
			FROM articles
			GROUP BY topic_group_id
		) g ON g.topic_group_id = a.topic_group_id
		WHERE a.is_primary = true
		  AND a.tags && $1::text[]
		  AND a.published_at >= now() - interval '14 days'`
	args := []any{tags}
	if excludeSeen && userID != "" {
		query += ` AND NOT EXISTS (
			SELECT 1 FROM user_seen_topics ust
			WHERE ust.user_id = $2::uuid AND ust.topic_group_id = a.topic_group_id
		)`
		args = append(args, userID)
	}
	query += ` ORDER BY a.published_at DESC LIMIT 30`

	rows, err := d.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select cached topics: %w", err)
	}
	defer rows.Close()

	topics := make([]domain.SelectedTopic, 0)
	for rows.Next() {
		var topic domain.SelectedTopic
		if err := rows.Scan(
			&topic.TopicGroupID,
			&topic.Primary.Title,
			&topic.Primary.Body,
			&topic.Primary.PublishedAt,
			&topic.Primary.SourceName,
			&topic.Primary.SourceURL,
			&topic.Primary.Tags,
			&topic.Primary.ShortenedTitle,
			&topic.Primary.Author,
			&topic.Primary.AbbreviatedBody,
			&topic.RelatedCount,
		); err != nil {
			return nil, fmt.Errorf("scan cached topic: %w", err)
		}
		topics = append(topics, topic)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cached topics: %w", err)
	}
	return topics, nil
}

func unionTags(left, right []string) []string {
	values := make(map[string]struct{}, len(left)+len(right))
	for _, tag := range append(append([]string(nil), left...), right...) {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			values[tag] = struct{}{}
		}
	}
	merged := make([]string, 0, len(values))
	for tag := range values {
		merged = append(merged, tag)
	}
	sort.Strings(merged)
	return merged
}

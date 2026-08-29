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
type CachedArticle struct {
	ID           string
	FeedID       string
	TopicGroupID string
	IsPrimary    bool
	Article      domain.Article
	Tags         []string
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

// FindArticleBySourceURL returns a previously cached article for an exact URL.
func (d *DB) FindArticleBySourceURL(ctx context.Context, sourceURL string) (CachedArticle, bool, error) {
	article, err := scanCachedArticle(d.pool.QueryRow(ctx, `
		SELECT id::text, feed_id, topic_group_id::text, is_primary, title, body,
		       published_at, source_name, source_url, tags
		FROM articles
		WHERE source_url = $1`, sourceURL))
	if err == nil {
		return article, true, nil
	}
	if err == pgx.ErrNoRows {
		return CachedArticle{}, false, nil
	}
	return CachedArticle{}, false, fmt.Errorf("find article by source URL: %w", err)
}

// MergeCachedArticleTags adds feed tags to an existing article and its
// group's primary article in one transaction.
func (d *DB) MergeCachedArticleTags(ctx context.Context, article CachedArticle, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin article tag merge: %w", err)
	}
	defer tx.Rollback(ctx)

	merged := unionTags(article.Tags, tags)
	if _, err := tx.Exec(ctx, `UPDATE articles SET tags = $2::text[] WHERE id = $1::uuid`, article.ID, merged); err != nil {
		return fmt.Errorf("update cached article tags: %w", err)
	}
	var primaryTags []string
	if err := tx.QueryRow(ctx, `
		SELECT tags FROM articles
		WHERE topic_group_id = $1::uuid AND is_primary = true
		FOR UPDATE`, article.TopicGroupID).Scan(&primaryTags); err != nil {
		return fmt.Errorf("find primary article for tag merge: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE articles SET tags = $2::text[]
		WHERE topic_group_id = $1::uuid AND is_primary = true`, article.TopicGroupID, unionTags(primaryTags, tags)); err != nil {
		return fmt.Errorf("update primary article tags: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit article tag merge: %w", err)
	}
	return nil
}

// RecentCachedArticles returns every article that may take part in duplicate
// matching. The caller compares titles, regardless of tags.
func (d *DB) RecentCachedArticles(ctx context.Context) ([]CachedArticle, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id::text, feed_id, topic_group_id::text, is_primary, title, body,
		       published_at, source_name, source_url, tags
		FROM articles
		WHERE published_at >= now() - interval '14 days'`)
	if err != nil {
		return nil, fmt.Errorf("list recent cached articles: %w", err)
	}
	defer rows.Close()

	articles := make([]CachedArticle, 0)
	for rows.Next() {
		article, err := scanCachedArticle(rows)
		if err != nil {
			return nil, err
		}
		articles = append(articles, article)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent cached articles: %w", err)
	}
	return articles, nil
}

// StoreIngestedArticle saves a new article and updates the matched group's
// primary article and tag union atomically. An empty matchedGroupID starts a
// new topic group using article.ID.
func (d *DB) StoreIngestedArticle(ctx context.Context, article CachedArticle, matchedGroupID string) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin article insert: %w", err)
	}
	defer tx.Rollback(ctx)

	groupID := matchedGroupID
	primary := matchedGroupID == ""
	if groupID == "" {
		groupID = article.ID
	}
	article.TopicGroupID = groupID
	if _, err := tx.Exec(ctx, `
		INSERT INTO articles (
			id, feed_id, topic_group_id, is_primary, title, body, published_at,
			source_name, source_url, tags
		) VALUES (
			$1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9, $10::text[]
		)`, article.ID, article.FeedID, groupID, primary, article.Article.Title,
		article.Article.Body, article.Article.PublishedAt, article.Article.SourceName,
		article.Article.SourceURL, article.Tags); err != nil {
		return fmt.Errorf("insert cached article: %w", err)
	}

	if !primary {
		var primaryID, primaryBody string
		if err := tx.QueryRow(ctx, `
			SELECT id::text, body FROM articles
			WHERE topic_group_id = $1::uuid AND is_primary = true
			FOR UPDATE`, groupID).Scan(&primaryID, &primaryBody); err != nil {
			return fmt.Errorf("find matched group primary: %w", err)
		}
		if utf8.RuneCountInString(article.Article.Body) > utf8.RuneCountInString(primaryBody) {
			if _, err := tx.Exec(ctx, `UPDATE articles SET is_primary = false WHERE id = $1::uuid`, primaryID); err != nil {
				return fmt.Errorf("clear previous group primary: %w", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE articles SET is_primary = true WHERE id = $1::uuid`, article.ID); err != nil {
				return fmt.Errorf("set replacement group primary: %w", err)
			}
		}
	}

	rows, err := tx.Query(ctx, `SELECT tags FROM articles WHERE topic_group_id = $1::uuid`, groupID)
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
		WHERE topic_group_id = $1::uuid AND is_primary = true`, groupID, allTags); err != nil {
		return fmt.Errorf("update group primary tags: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cached article: %w", err)
	}
	return nil
}

// SelectCachedTopics returns primary article groups matching user tags. When
// excludeSeen is true, already delivered topic groups are omitted.
func (d *DB) SelectCachedTopics(ctx context.Context, tags []string, userID string, excludeSeen bool) ([]domain.Topic, error) {
	query := `
		SELECT a.topic_group_id::text, a.title, a.body, a.published_at,
		       a.source_name, a.source_url, g.related_count
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

	topics := make([]domain.Topic, 0)
	for rows.Next() {
		var topic domain.Topic
		if err := rows.Scan(
			&topic.TopicGroupID,
			&topic.Primary.Title,
			&topic.Primary.Body,
			&topic.Primary.PublishedAt,
			&topic.Primary.SourceName,
			&topic.Primary.SourceURL,
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

func scanCachedArticle(row pgx.Row) (CachedArticle, error) {
	var article CachedArticle
	err := row.Scan(
		&article.ID,
		&article.FeedID,
		&article.TopicGroupID,
		&article.IsPrimary,
		&article.Article.Title,
		&article.Article.Body,
		&article.Article.PublishedAt,
		&article.Article.SourceName,
		&article.Article.SourceURL,
		&article.Tags,
	)
	if err != nil {
		return CachedArticle{}, err
	}
	return article, nil
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

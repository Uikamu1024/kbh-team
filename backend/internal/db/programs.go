package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

// ErrProgramNotFound indicates that the requested program does not exist.
var ErrProgramNotFound = errors.New("program not found")

// Program contains a programs row and fields derived from its chapters.
type Program struct {
	ID               string
	UserID           string
	CreatedAt        time.Time
	GreetingText     string
	ChangeCount      int
	Title            string
	TotalDurationSec int
}

// Chapter represents a chapters row.
type Chapter struct {
	ID                  string
	ProgramID           string
	Position            int
	Title               string
	SourceURL           string
	SourceName          string
	Script              string
	AudioPath           string
	DurationSec         int
	LineStartOffsetsSec []float64
}

// ProgramSummary contains the lightweight fields needed by program history.
type ProgramSummary struct {
	ID               string
	Title            string
	CreatedAt        time.Time
	TotalDurationSec int
}

// HasProgramSince reports whether a user has a program created at or after
// since. Batch generation uses the delivery-day boundary supplied by the API.
func (d *DB) HasProgramSince(ctx context.Context, userID string, since time.Time) (bool, error) {
	var exists bool
	if err := d.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM programs
			WHERE user_id = $1::uuid AND created_at >= $2
		)`, userID, since).Scan(&exists); err != nil {
		return false, fmt.Errorf("check programs since %s: %w", since.Format(time.RFC3339), err)
	}
	return exists, nil
}

// CreateProgram inserts a program and all of its chapters atomically.
func (d *DB) CreateProgram(ctx context.Context, userID, greetingText string, changeCount int, chapters []domain.ChapterAudio, chapterAudioPaths []string) (string, error) {
	if len(chapters) != len(chapterAudioPaths) {
		return "", fmt.Errorf("chapter count %d does not match audio path count %d", len(chapters), len(chapterAudioPaths))
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin program transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	programID, err := insertProgram(ctx, tx, userID, greetingText, changeCount, chapters, chapterAudioPaths)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit program: %w", err)
	}
	return programID, nil
}

// CreateProgramWithIDs is used when audio must be saved before its database
// rows are committed. The caller owns the UUIDs and final audio paths.
func (d *DB) CreateProgramWithIDs(ctx context.Context, userID, greetingText string, changeCount int, chapters []domain.ChapterAudio, chapterAudioPaths []string, programID string, chapterIDs []string) error {
	return d.CreateProgramWithIDsAndSeenTopics(ctx, userID, greetingText, changeCount, chapters, chapterAudioPaths, programID, chapterIDs, nil)
}

// CreateProgramWithIDsAndSeenTopics creates a program and records its selected
// topic groups in the same transaction.
func (d *DB) CreateProgramWithIDsAndSeenTopics(ctx context.Context, userID, greetingText string, changeCount int, chapters []domain.ChapterAudio, chapterAudioPaths []string, programID string, chapterIDs []string, seenTopicGroupIDs []string) error {
	if len(chapters) != len(chapterAudioPaths) {
		return fmt.Errorf("chapter count %d does not match audio path count %d", len(chapters), len(chapterAudioPaths))
	}
	if len(chapters) != len(chapterIDs) {
		return fmt.Errorf("chapter count %d does not match chapter ID count %d", len(chapters), len(chapterIDs))
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin program transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, _, err := insertProgramWithIDs(ctx, tx, userID, greetingText, changeCount, chapters, chapterAudioPaths, programID, chapterIDs); err != nil {
		return err
	}
	if err := insertSeenTopics(ctx, tx, userID, seenTopicGroupIDs); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit program: %w", err)
	}
	return nil
}

// GetLatestProgramByUser returns the newest program and its chapters.
func (d *DB) GetLatestProgramByUser(ctx context.Context, userID string) (Program, []Chapter, error) {
	if err := d.requireUser(ctx, userID); err != nil {
		return Program{}, nil, err
	}

	program, err := d.queryProgram(ctx, `
		SELECT id::text, user_id::text, created_at, greeting_text, change_count
		FROM programs
		WHERE user_id = $1::uuid
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Program{}, nil, ErrProgramNotFound
	}
	if err != nil {
		return Program{}, nil, err
	}
	chapters, err := d.getChapters(ctx, program.ID)
	if err != nil {
		return Program{}, nil, err
	}
	populateProgramFields(&program, chapters)
	return program, chapters, nil
}

// ListProgramsByUser returns recent program summaries and the unpaginated total.
func (d *DB) ListProgramsByUser(ctx context.Context, userID string, limit int) ([]ProgramSummary, int, error) {
	if err := d.requireUser(ctx, userID); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	var total int64
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM programs WHERE user_id = $1::uuid`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count user programs: %w", err)
	}

	rows, err := d.pool.Query(ctx, `
		SELECT p.id::text,
		       COALESCE((
			       SELECT c.title FROM chapters c
			       WHERE c.program_id = p.id
			       ORDER BY c.position
			       LIMIT 1
		       ), ''),
		       p.created_at,
		       COALESCE((
			       SELECT sum(c.duration_sec) FROM chapters c
			       WHERE c.program_id = p.id
		       ), 0)
		FROM programs p
		WHERE p.user_id = $1::uuid
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list user programs: %w", err)
	}
	defer rows.Close()

	programs := make([]ProgramSummary, 0)
	for rows.Next() {
		var program ProgramSummary
		var duration int64
		if err := rows.Scan(&program.ID, &program.Title, &program.CreatedAt, &duration); err != nil {
			return nil, 0, fmt.Errorf("scan program summary: %w", err)
		}
		program.TotalDurationSec = int(duration)
		programs = append(programs, program)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate program summaries: %w", err)
	}
	return programs, int(total), nil
}

// GetProgramByID returns a program and all of its chapters.
func (d *DB) GetProgramByID(ctx context.Context, programID string) (Program, []Chapter, error) {
	program, err := d.queryProgram(ctx, `
		SELECT id::text, user_id::text, created_at, greeting_text, change_count
		FROM programs
		WHERE id = $1::uuid`, programID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Program{}, nil, ErrProgramNotFound
	}
	if err != nil {
		return Program{}, nil, err
	}
	chapters, err := d.getChapters(ctx, program.ID)
	if err != nil {
		return Program{}, nil, err
	}
	populateProgramFields(&program, chapters)
	return program, chapters, nil
}

// ReplaceLatestProgramWithIDs is the storage-coordinated variant of
// ReplaceLatestProgram. The caller supplies IDs so audio can be written to
// its final paths before this transaction changes the database.
func (d *DB) ReplaceLatestProgramWithIDs(ctx context.Context, userID, greetingText string, changeCount int, chapters []domain.ChapterAudio, chapterAudioPaths []string, programID string, chapterIDs []string) error {
	return d.ReplaceLatestProgramWithIDsAndSeenTopics(ctx, userID, greetingText, changeCount, chapters, chapterAudioPaths, programID, chapterIDs, nil)
}

// ReplaceLatestProgramWithIDsAndSeenTopics replaces the newest program and
// records the replacement's selected topic groups in one transaction.
func (d *DB) ReplaceLatestProgramWithIDsAndSeenTopics(ctx context.Context, userID, greetingText string, changeCount int, chapters []domain.ChapterAudio, chapterAudioPaths []string, programID string, chapterIDs []string, seenTopicGroupIDs []string) error {
	_, _, err := d.replaceLatestProgram(ctx, userID, greetingText, changeCount, chapters, chapterAudioPaths, programID, chapterIDs, seenTopicGroupIDs)
	return err
}

func (d *DB) replaceLatestProgram(ctx context.Context, userID, greetingText string, changeCount int, chapters []domain.ChapterAudio, chapterAudioPaths []string, requestedProgramID string, requestedChapterIDs []string, seenTopicGroupIDs []string) (string, []string, error) {
	if len(chapters) != len(chapterAudioPaths) {
		return "", nil, fmt.Errorf("chapter count %d does not match audio path count %d", len(chapters), len(chapterAudioPaths))
	}
	if len(requestedChapterIDs) != 0 && len(chapters) != len(requestedChapterIDs) {
		return "", nil, fmt.Errorf("chapter count %d does not match chapter ID count %d", len(chapters), len(requestedChapterIDs))
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("begin replace program transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var userExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1::uuid)`, userID).Scan(&userExists); err != nil {
		return "", nil, fmt.Errorf("check user for program replacement: %w", err)
	}
	if !userExists {
		return "", nil, ErrUserNotFound
	}

	var latestID string
	err = tx.QueryRow(ctx, `
		SELECT id::text
		FROM programs
		WHERE user_id = $1::uuid
		ORDER BY created_at DESC, id DESC
		LIMIT 1
		FOR UPDATE`, userID).Scan(&latestID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", nil, fmt.Errorf("find latest program for replacement: %w", err)
	}
	if err == nil {
		if _, err := tx.Exec(ctx, `DELETE FROM chapters WHERE program_id = $1::uuid`, latestID); err != nil {
			return "", nil, fmt.Errorf("delete latest program chapters: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM programs WHERE id = $1::uuid`, latestID); err != nil {
			return "", nil, fmt.Errorf("delete latest program: %w", err)
		}
	}

	programID, chapterIDs, err := insertProgramWithIDs(ctx, tx, userID, greetingText, changeCount, chapters, chapterAudioPaths, requestedProgramID, requestedChapterIDs)
	if err != nil {
		return "", nil, err
	}
	if err := insertSeenTopics(ctx, tx, userID, seenTopicGroupIDs); err != nil {
		return "", nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", nil, fmt.Errorf("commit replaced program: %w", err)
	}
	return programID, chapterIDs, nil
}

func insertProgram(ctx context.Context, tx pgx.Tx, userID, greetingText string, changeCount int, chapters []domain.ChapterAudio, chapterAudioPaths []string) (string, error) {
	programID, _, err := insertProgramWithIDs(ctx, tx, userID, greetingText, changeCount, chapters, chapterAudioPaths, "", nil)
	return programID, err
}

func insertProgramWithIDs(ctx context.Context, tx pgx.Tx, userID, greetingText string, changeCount int, chapters []domain.ChapterAudio, chapterAudioPaths []string, requestedProgramID string, requestedChapterIDs []string) (string, []string, error) {
	programID := requestedProgramID
	var err error
	if programID == "" {
		programID, err = newUUID()
	}
	if err != nil {
		return "", nil, fmt.Errorf("generate program ID: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO programs (id, user_id, greeting_text, change_count)
		VALUES ($1::uuid, $2::uuid, $3, $4)`, programID, userID, greetingText, changeCount); err != nil {
		return "", nil, fmt.Errorf("insert program: %w", err)
	}

	chapterIDs := make([]string, len(chapters))
	for index, chapter := range chapters {
		chapterID := ""
		if len(requestedChapterIDs) != 0 {
			chapterID = requestedChapterIDs[index]
		} else {
			chapterID, err = newUUID()
			if err != nil {
				return "", nil, fmt.Errorf("generate chapter ID: %w", err)
			}
		}
		chapterIDs[index] = chapterID
		// NOT NULL column: a nil slice (e.g. a chapter with no lines) must
		// be sent as an empty array, not SQL NULL (same reasoning as
		// articles.go's tags nil-guard).
		offsets := chapter.LineStartOffsetsSec
		if offsets == nil {
			offsets = []float64{}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO chapters (
				id, program_id, position, title, source_url, source_name,
				script, audio_path, duration_sec, line_start_offsets_sec
			)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10::double precision[])`,
			chapterID,
			programID,
			chapter.Position,
			chapter.Primary.Title,
			chapter.Primary.SourceURL,
			chapter.Primary.SourceName,
			chapterScript(chapter.Lines),
			chapterAudioPaths[index],
			chapter.DurationSec,
			offsets,
		); err != nil {
			return "", nil, fmt.Errorf("insert chapter %d: %w", index, err)
		}
	}
	return programID, chapterIDs, nil
}

func (d *DB) queryProgram(ctx context.Context, query, programID string) (Program, error) {
	var program Program
	err := d.pool.QueryRow(ctx, query, programID).Scan(
		&program.ID,
		&program.UserID,
		&program.CreatedAt,
		&program.GreetingText,
		&program.ChangeCount,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Program{}, pgx.ErrNoRows
		}
		return Program{}, fmt.Errorf("get program: %w", err)
	}
	return program, nil
}

func (d *DB) getChapters(ctx context.Context, programID string) ([]Chapter, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id::text, program_id::text, position, title, source_url, source_name,
		       script, audio_path, duration_sec, line_start_offsets_sec
		FROM chapters
		WHERE program_id = $1::uuid
		ORDER BY position`, programID)
	if err != nil {
		return nil, fmt.Errorf("get program chapters: %w", err)
	}
	defer rows.Close()

	chapters := make([]Chapter, 0)
	for rows.Next() {
		var chapter Chapter
		if err := rows.Scan(
			&chapter.ID,
			&chapter.ProgramID,
			&chapter.Position,
			&chapter.Title,
			&chapter.SourceURL,
			&chapter.SourceName,
			&chapter.Script,
			&chapter.AudioPath,
			&chapter.DurationSec,
			&chapter.LineStartOffsetsSec,
		); err != nil {
			return nil, fmt.Errorf("scan program chapter: %w", err)
		}
		chapters = append(chapters, chapter)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate program chapters: %w", err)
	}
	return chapters, nil
}

func (d *DB) requireUser(ctx context.Context, userID string) error {
	var exists bool
	if err := d.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1::uuid)`, userID).Scan(&exists); err != nil {
		return fmt.Errorf("check user: %w", err)
	}
	if !exists {
		return ErrUserNotFound
	}
	return nil
}

func populateProgramFields(program *Program, chapters []Chapter) {
	program.TotalDurationSec = 0
	for index, chapter := range chapters {
		program.TotalDurationSec += chapter.DurationSec
		if index == 0 || chapter.Position == 0 {
			program.Title = chapter.Title
		}
	}
}

func chapterScript(lines []domain.Line) string {
	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.Text)
	}
	return strings.Join(texts, "\n")
}

func insertSeenTopics(ctx context.Context, tx pgx.Tx, userID string, topicGroupIDs []string) error {
	for _, topicGroupID := range topicGroupIDs {
		if strings.TrimSpace(topicGroupID) == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_seen_topics (user_id, topic_group_id)
			VALUES ($1::uuid, $2::uuid)
			ON CONFLICT (user_id, topic_group_id) DO NOTHING`, userID, topicGroupID); err != nil {
			return fmt.Errorf("insert seen topic %q: %w", topicGroupID, err)
		}
	}
	return nil
}

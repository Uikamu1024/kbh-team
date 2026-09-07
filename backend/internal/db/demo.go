package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AssignRandomDemoProgram is DEMO_MODE's replacement for real generation: it
// picks a random program not yet assigned to userID and records the
// assignment in demo_program_assignments, without touching programs/chapters
// at all. Returns ErrResetLimitExceeded once every existing program has
// already been assigned to this user (the demo pool is exhausted for them),
// reusing the same error the real regenerate path uses for its 3/day limit
// since both represent "nothing new available right now."
func (d *DB) AssignRandomDemoProgram(ctx context.Context, userID string) (string, time.Time, error) {
	if err := d.requireUser(ctx, userID); err != nil {
		return "", time.Time{}, err
	}

	var programID string
	err := d.pool.QueryRow(ctx, `
		SELECT p.id::text
		FROM programs p
		WHERE NOT EXISTS (
			SELECT 1 FROM demo_program_assignments a
			WHERE a.user_id = $1::uuid AND a.program_id = p.id
		)
		ORDER BY random()
		LIMIT 1`, userID).Scan(&programID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, ErrResetLimitExceeded
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("pick random demo program: %w", err)
	}

	var assignedAt time.Time
	if err := d.pool.QueryRow(ctx, `
		INSERT INTO demo_program_assignments (user_id, program_id)
		VALUES ($1::uuid, $2::uuid)
		RETURNING assigned_at`, userID, programID).Scan(&assignedAt); err != nil {
		return "", time.Time{}, fmt.Errorf("assign demo program: %w", err)
	}
	return programID, assignedAt, nil
}

// GetLatestDemoProgramForUser returns the most recently assigned demo
// program's ID and assignment time for userID, or ErrProgramNotFound if none
// has been assigned yet.
func (d *DB) GetLatestDemoProgramForUser(ctx context.Context, userID string) (string, time.Time, error) {
	if err := d.requireUser(ctx, userID); err != nil {
		return "", time.Time{}, err
	}
	var programID string
	var assignedAt time.Time
	err := d.pool.QueryRow(ctx, `
		SELECT program_id::text, assigned_at
		FROM demo_program_assignments
		WHERE user_id = $1::uuid
		ORDER BY assigned_at DESC
		LIMIT 1`, userID).Scan(&programID, &assignedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, ErrProgramNotFound
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("get latest demo program: %w", err)
	}
	return programID, assignedAt, nil
}

// ListDemoProgramsForUser returns program summaries for every program
// assigned to userID (most recently assigned first) and the total count.
// CreatedAt on each summary is the assignment time, not the program's real
// original creation time, so the demo history reads as "when this user got
// it" rather than exposing whenever the underlying program was first
// generated.
func (d *DB) ListDemoProgramsForUser(ctx context.Context, userID string, limit int) ([]ProgramSummary, int, error) {
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
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM demo_program_assignments WHERE user_id = $1::uuid`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count demo programs: %w", err)
	}

	rows, err := d.pool.Query(ctx, `
		SELECT p.id::text,
		       COALESCE((
			       SELECT c.title FROM chapters c
			       WHERE c.program_id = p.id
			       ORDER BY c.position
			       LIMIT 1
		       ), ''),
		       a.assigned_at,
		       COALESCE((
			       SELECT sum(c.duration_sec) FROM chapters c
			       WHERE c.program_id = p.id
		       ), 0)
		FROM demo_program_assignments a
		JOIN programs p ON p.id = a.program_id
		WHERE a.user_id = $1::uuid
		ORDER BY a.assigned_at DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list demo programs: %w", err)
	}
	defer rows.Close()

	programs := make([]ProgramSummary, 0)
	for rows.Next() {
		var program ProgramSummary
		var duration int64
		if err := rows.Scan(&program.ID, &program.Title, &program.CreatedAt, &duration); err != nil {
			return nil, 0, fmt.Errorf("scan demo program summary: %w", err)
		}
		program.TotalDurationSec = int(duration)
		programs = append(programs, program)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate demo program summaries: %w", err)
	}
	return programs, int(total), nil
}

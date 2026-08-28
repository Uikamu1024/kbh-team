package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrUserNotFound indicates that the requested user does not exist.
	ErrUserNotFound = errors.New("user not found")
	// ErrResetLimitExceeded indicates that the user has already used three resets for the date.
	ErrResetLimitExceeded = errors.New("daily reset limit exceeded")
)

// User represents all columns in the users table.
type User struct {
	ID            string
	Tags          []string
	DeliveryTime  string
	LengthMinutes int
	ResetCount    int
	ResetDate     *time.Time
}

// ListAllUsers returns every user for the small, single-process batch job.
func (d *DB) ListAllUsers(ctx context.Context) ([]User, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id::text, tags, to_char(delivery_time, 'HH24:MI'), length_minutes, reset_count,
		       COALESCE(reset_date::text, '')
		FROM users
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		var user User
		var resetDate string
		if err := rows.Scan(&user.ID, &user.Tags, &user.DeliveryTime, &user.LengthMinutes, &user.ResetCount, &resetDate); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		if resetDate != "" {
			parsed, err := time.Parse("2006-01-02", resetDate)
			if err != nil {
				return nil, fmt.Errorf("parse user reset date: %w", err)
			}
			user.ResetDate = &parsed
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

// CreateUser inserts a user with the schema defaults and returns its UUID.
func (d *DB) CreateUser(ctx context.Context) (string, error) {
	userID, err := newUUID()
	if err != nil {
		return "", fmt.Errorf("generate user ID: %w", err)
	}
	if _, err := d.pool.Exec(ctx, `INSERT INTO users (id) VALUES ($1::uuid)`, userID); err != nil {
		return "", fmt.Errorf("insert user: %w", err)
	}
	return userID, nil
}

// GetUser retrieves a user by UUID.
func (d *DB) GetUser(ctx context.Context, userID string) (User, error) {
	var user User
	var resetDate string
	err := d.pool.QueryRow(ctx, `
		SELECT id::text, tags, to_char(delivery_time, 'HH24:MI'), length_minutes, reset_count,
		       COALESCE(reset_date::text, '')
		FROM users
		WHERE id = $1::uuid`, userID).Scan(
		&user.ID,
		&user.Tags,
		&user.DeliveryTime,
		&user.LengthMinutes,
		&user.ResetCount,
		&resetDate,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	if resetDate != "" {
		parsed, err := time.Parse("2006-01-02", resetDate)
		if err != nil {
			return User{}, fmt.Errorf("parse user reset date: %w", err)
		}
		user.ResetDate = &parsed
	}
	return user, nil
}

// UpdateUserTags replaces the user's selected tags.
func (d *DB) UpdateUserTags(ctx context.Context, userID string, tags []string) error {
	result, err := d.pool.Exec(ctx, `UPDATE users SET tags = $2::text[] WHERE id = $1::uuid`, userID, tags)
	if err != nil {
		return fmt.Errorf("update user tags: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// UpdateUserSettings updates the delivery time and program length settings.
func (d *DB) UpdateUserSettings(ctx context.Context, userID, deliveryTime string, lengthMinutes int) error {
	result, err := d.pool.Exec(ctx, `
		UPDATE users
		SET delivery_time = $2::time, length_minutes = $3
		WHERE id = $1::uuid`, userID, deliveryTime, lengthMinutes)
	if err != nil {
		return fmt.Errorf("update user settings: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// IncrementResetCount increments today's reset count atomically. A new date
// starts at zero before the increment, and the fourth reset is rejected.
func (d *DB) IncrementResetCount(ctx context.Context, userID string, today time.Time) (int, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin reset transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var resetCount int
	var resetDate string
	err = tx.QueryRow(ctx, `
		SELECT reset_count, COALESCE(reset_date::text, '')
		FROM users
		WHERE id = $1::uuid
		FOR UPDATE`, userID).Scan(&resetCount, &resetDate)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrUserNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("read reset count: %w", err)
	}

	todayDate := today.Format("2006-01-02")
	if resetDate != todayDate {
		resetCount = 0
	}
	if resetCount >= 3 {
		return 0, ErrResetLimitExceeded
	}

	newCount := resetCount + 1
	if _, err := tx.Exec(ctx, `
		UPDATE users
		SET reset_count = $2, reset_date = $3::date
		WHERE id = $1::uuid`, userID, newCount, todayDate); err != nil {
		return 0, fmt.Errorf("update reset count: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit reset count: %w", err)
	}
	return newCount, nil
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	var encoded [36]byte
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded[:]), nil
}

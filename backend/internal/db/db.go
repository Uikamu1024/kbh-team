package db

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/0001_init.sql
var initialMigration string

// DB wraps the PostgreSQL connection pool used by the application.
type DB struct {
	pool *pgxpool.Pool
}

// Open creates a connection pool, verifies the connection, and applies the
// embedded idempotent schema migration.
func Open(ctx context.Context, databaseURL string) (*DB, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	database := &DB{pool: pool}
	if err := database.Ping(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if _, err := pool.Exec(ctx, initialMigration); err != nil {
		database.Close()
		return nil, fmt.Errorf("apply database migration: %w", err)
	}
	return database, nil
}

// Close releases all connections held by the pool.
func (d *DB) Close() {
	if d != nil && d.pool != nil {
		d.pool.Close()
	}
}

// Ping verifies that the database is reachable.
func (d *DB) Ping(ctx context.Context) error {
	if d == nil || d.pool == nil {
		return fmt.Errorf("database is not initialized")
	}
	return d.pool.Ping(ctx)
}

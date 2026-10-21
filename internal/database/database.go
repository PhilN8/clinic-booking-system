// Package database opens the Postgres connection pool and applies the
// embedded migrations.
//
// Migrations are plain .sql files embedded into the binary and applied
// idempotently on boot. That keeps local development, CI and the deployed
// container on exactly the same code path with no external CLI to install.
package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const migrationDir = "migrations"

// migrationLockKey is an arbitrary but stable key for the Postgres advisory
// lock that serialises migrations. It only has to be unique within this
// database, not globally meaningful.
const migrationLockKey int64 = 8_073_451_996_233

// Open connects to Postgres, verifies the connection and applies any pending
// migrations. The returned pool is ready for use.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("database: open: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(time.Hour)
	db.SetConnMaxIdleTime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}

	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// Migrate applies every embedded migration that has not been applied yet.
//
// It takes a single dedicated connection so that the advisory lock is held
// on the same session that runs the migrations. A pool would be free to
// release the advisory lock on a different connection, which would defeat
// the lock entirely.
func Migrate(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("database: acquire migration connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("database: acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockKey)
	}()

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("database: create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, migrationDir)
	if err != nil {
		return fmt.Errorf("database: read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := migrationApplied(ctx, conn, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, conn, name); err != nil {
			return err
		}
	}

	return nil
}

func migrationApplied(ctx context.Context, conn *sql.Conn, version string) (bool, error) {
	var applied bool
	err := conn.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied)
	if err != nil {
		return false, fmt.Errorf("database: check migration %s: %w", version, err)
	}
	return applied, nil
}

func applyMigration(ctx context.Context, conn *sql.Conn, version string) error {
	body, err := fs.ReadFile(migrationFS, migrationDir+"/"+version)
	if err != nil {
		return fmt.Errorf("database: read migration %s: %w", version, err)
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("database: begin migration %s: %w", version, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("database: apply migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return fmt.Errorf("database: record migration %s: %w", version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("database: commit migration %s: %w", version, err)
	}
	return nil
}

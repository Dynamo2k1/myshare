// Package store owns the SQLite database: connection setup, schema migration,
// and typed queries for every entity MyShare persists.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed all:migrations
var migrationFS embed.FS

// DB wraps *sql.DB with MyShare's helpers.
type DB struct {
	*sql.DB
	path string
}

// Open connects to the SQLite database at path, applies pragmas, and runs any
// outstanding migrations. wal selects journal mode: pass false when the data
// directory is on a filesystem with unreliable POSIX locking (fuseblk/NTFS),
// where WAL can corrupt or hang.
func Open(ctx context.Context, path string, wal bool) (*DB, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// modernc's driver is safe for concurrent use, but a single writer avoids
	// SQLITE_BUSY churn. Reads still parallelise at the SQLite level.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetConnMaxLifetime(0)

	db := &DB{DB: sqlDB, path: path}

	journal := "WAL"
	if !wal {
		journal = "TRUNCATE"
	}
	pragmas := []string{
		"PRAGMA journal_mode = " + journal,
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA temp_store = MEMORY",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}

	if err := db.migrate(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// Path returns the on-disk database file path.
func (db *DB) Path() string { return db.path }

func (db *DB) migrate(ctx context.Context) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := map[int]bool{}
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	type mig struct {
		version int
		name    string
		body    string
	}
	var migs []mig
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		ver, err := parseVersion(e.Name())
		if err != nil {
			return err
		}
		b, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		migs = append(migs, mig{ver, e.Name(), string(b)})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })

	for _, m := range migs {
		if applied[m.version] {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.body); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
			m.version, time.Now().Unix()); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.name, err)
		}
	}
	return nil
}

func parseVersion(name string) (int, error) {
	i := strings.IndexByte(name, '_')
	if i <= 0 {
		return 0, fmt.Errorf("migration %q: expected NNNN_name.sql", name)
	}
	n := 0
	for _, r := range name[:i] {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("migration %q: non-numeric version", name)
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// now is a package-level clock indirection so tests can pin time.
var now = func() int64 { return time.Now().Unix() }

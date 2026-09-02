package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

func itoa(n int) string { return strconv.Itoa(n) }

// Stats is a snapshot of aggregate counts for the status page.
type Stats struct {
	Files       int64 `json:"files"`
	Screenshots int64 `json:"screenshots"`
	Clipboard   int64 `json:"clipboard"`
	Snippets    int64 `json:"snippets"`
	Notes       int64 `json:"notes"`
	Shares      int64 `json:"shares"`
	BlobBytes   int64 `json:"blob_bytes"`
	BlobCount   int64 `json:"blob_count"`
}

// Stats gathers aggregate counts in one round of queries.
func (db *DB) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	q := func(dst *int64, query string, args ...any) error {
		return db.QueryRowContext(ctx, query, args...).Scan(dst)
	}
	if err := q(&s.Files,
		`SELECT COUNT(*) FROM files WHERE deleted_at IS NULL AND kind = 'file'`); err != nil {
		return s, err
	}
	if err := q(&s.Screenshots,
		`SELECT COUNT(*) FROM files WHERE deleted_at IS NULL AND kind = 'screenshot'`); err != nil {
		return s, err
	}
	if err := q(&s.Clipboard, `SELECT COUNT(*) FROM clipboard`); err != nil {
		return s, err
	}
	if err := q(&s.Snippets, `SELECT COUNT(*) FROM snippets`); err != nil {
		return s, err
	}
	if err := q(&s.Notes, `SELECT COUNT(*) FROM notes`); err != nil {
		return s, err
	}
	if err := q(&s.Shares, `SELECT COUNT(*) FROM shares WHERE revoked_at IS NULL`); err != nil {
		return s, err
	}
	if err := q(&s.BlobCount, `SELECT COUNT(*) FROM blobs`); err != nil {
		return s, err
	}
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(size),0) FROM blobs`).Scan(&s.BlobBytes); err != nil {
		return s, err
	}
	return s, nil
}

// GetSetting reads a settings value. Missing key returns ("", nil).
func (db *DB) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetSetting writes a settings value.
func (db *DB) SetSetting(ctx context.Context, key, value string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO settings(key,value,updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, now())
	return err
}

// DeleteSetting removes a settings key.
func (db *DB) DeleteSetting(ctx context.Context, key string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	return err
}

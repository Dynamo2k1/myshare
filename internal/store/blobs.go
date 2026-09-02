package store

import (
	"context"
	"database/sql"
	"errors"
)

// BlobExists reports whether a blob row for hash is present.
func (db *DB) BlobExists(ctx context.Context, hash string) (bool, error) {
	var one int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM blobs WHERE hash = ?`, hash).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// UpsertBlobRef inserts the blob row if new and increments its refcount. It
// returns whether the blob already existed (so the caller can discard freshly
// uploaded bytes that duplicate an existing blob).
func (db *DB) UpsertBlobRef(ctx context.Context, tx *sql.Tx, hash string, size int64) (existed bool, err error) {
	var cur int64
	err = tx.QueryRowContext(ctx, `SELECT refcount FROM blobs WHERE hash = ?`, hash).Scan(&cur)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx,
			`INSERT INTO blobs(hash, size, refcount, created_at) VALUES(?, ?, 1, ?)`,
			hash, size, now())
		return false, err
	case err != nil:
		return false, err
	default:
		_, err = tx.ExecContext(ctx, `UPDATE blobs SET refcount = refcount + 1 WHERE hash = ?`, hash)
		return true, err
	}
}

// ReleaseBlob decrements a blob's refcount within tx and reports whether the
// blob is now unreferenced (refcount <= 0), meaning the on-disk bytes may be
// deleted. The row itself is removed here when it reaches zero.
func (db *DB) ReleaseBlob(ctx context.Context, tx *sql.Tx, hash string) (orphaned bool, err error) {
	var rc int64
	err = tx.QueryRowContext(ctx, `SELECT refcount FROM blobs WHERE hash = ?`, hash).Scan(&rc)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	rc--
	if rc > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE blobs SET refcount = ? WHERE hash = ?`, rc, hash)
		return false, err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM blobs WHERE hash = ?`, hash)
	return true, err
}

// OrphanBlobHashes returns blob hashes with a non-positive refcount, for the
// cleanup sweep. Normally ReleaseBlob deletes such rows immediately; this
// catches any left behind by an interrupted transaction.
func (db *DB) OrphanBlobHashes(ctx context.Context, limit int) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT hash FROM blobs WHERE refcount <= 0 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// DeleteBlobRow removes a blob metadata row unconditionally (used by cleanup
// after the on-disk file is gone).
func (db *DB) DeleteBlobRow(ctx context.Context, hash string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM blobs WHERE hash = ?`, hash)
	return err
}

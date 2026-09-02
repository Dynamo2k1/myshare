package store

import (
	"context"
	"database/sql"
	"errors"
)

// ErrShareUnavailable means the share exists but is revoked, expired, or has
// hit its download cap.
var ErrShareUnavailable = errors.New("share unavailable")

// ShareCreateOptions configures CreateShare.
type ShareCreateOptions struct {
	FileID       string
	TokenHash    string // caller supplies sha-256 of the secret token
	ExpiresAt    *int64
	MaxDownloads *int64
	OneTime      bool
}

// CreateShare inserts a share row. The plaintext token is never persisted.
func (db *DB) CreateShare(ctx context.Context, opt ShareCreateOptions) (Share, error) {
	s := Share{
		ID: NewID(), TokenHash: opt.TokenHash, FileID: opt.FileID,
		CreatedAt: now(), ExpiresAt: opt.ExpiresAt,
		MaxDownloads: opt.MaxDownloads, OneTime: opt.OneTime,
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO shares(id,token_hash,file_id,created_at,expires_at,max_downloads,downloads,one_time)
		 VALUES(?,?,?,?,?,?,0,?)`,
		s.ID, s.TokenHash, s.FileID, s.CreatedAt, s.ExpiresAt, s.MaxDownloads, s.OneTime)
	return s, err
}

// ListSharesForFile returns all non-revoked shares for a file.
func (db *DB) ListSharesForFile(ctx context.Context, fileID string) ([]Share, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id,token_hash,file_id,created_at,expires_at,max_downloads,downloads,one_time,revoked_at
		   FROM shares WHERE file_id = ? AND revoked_at IS NULL ORDER BY created_at DESC`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Share
	for rows.Next() {
		s, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RevokeShare marks a share revoked. Idempotent.
func (db *DB) RevokeShare(ctx context.Context, id string) error {
	res, err := db.ExecContext(ctx,
		`UPDATE shares SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Either missing or already revoked; treat missing as not-found.
		var one int
		e := db.QueryRowContext(ctx, `SELECT 1 FROM shares WHERE id = ?`, id).Scan(&one)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
	}
	return nil
}

// ResolveShareToken looks up a live share by token hash and returns it together
// with its target file. It enforces expiry, revocation and the download cap but
// does NOT increment the counter — call RecordShareDownload after streaming.
func (db *DB) ResolveShareToken(ctx context.Context, tokenHash string) (Share, File, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id,token_hash,file_id,created_at,expires_at,max_downloads,downloads,one_time,revoked_at
		   FROM shares WHERE token_hash = ?`, tokenHash)
	s, err := scanShare(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Share{}, File{}, ErrNotFound
	}
	if err != nil {
		return Share{}, File{}, err
	}
	if s.RevokedAt != nil {
		return Share{}, File{}, ErrShareUnavailable
	}
	if s.ExpiresAt != nil && *s.ExpiresAt <= now() {
		return Share{}, File{}, ErrShareUnavailable
	}
	if s.MaxDownloads != nil && s.Downloads >= *s.MaxDownloads {
		return Share{}, File{}, ErrShareUnavailable
	}
	f, err := db.GetFile(ctx, s.FileID)
	if err != nil {
		return Share{}, File{}, ErrShareUnavailable // file deleted
	}
	return s, f, nil
}

// RecordShareDownload increments the counter and, for one-time shares or shares
// that have now reached their cap, revokes them.
func (db *DB) RecordShareDownload(ctx context.Context, id string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var downloads int64
	var maxDL sql.NullInt64
	var oneTime bool
	if err := tx.QueryRowContext(ctx,
		`SELECT downloads, max_downloads, one_time FROM shares WHERE id = ?`, id).
		Scan(&downloads, &maxDL, &oneTime); err != nil {
		return err
	}
	downloads++
	revoke := oneTime || (maxDL.Valid && downloads >= maxDL.Int64)
	if revoke {
		_, err = tx.ExecContext(ctx,
			`UPDATE shares SET downloads = ?, revoked_at = ? WHERE id = ?`, downloads, now(), id)
	} else {
		_, err = tx.ExecContext(ctx,
			`UPDATE shares SET downloads = ? WHERE id = ?`, downloads, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ExpiredShareIDs returns revoked-or-expired share IDs for cleanup.
func (db *DB) ExpiredShareIDs(ctx context.Context, limit int) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM shares
		  WHERE revoked_at IS NOT NULL
		     OR (expires_at IS NOT NULL AND expires_at <= ?)
		  LIMIT ?`, now(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DeleteShare hard-removes a share row.
func (db *DB) DeleteShare(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM shares WHERE id = ?`, id)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanShare(row scanner) (Share, error) {
	var s Share
	var exp, maxDL, rev sql.NullInt64
	err := row.Scan(&s.ID, &s.TokenHash, &s.FileID, &s.CreatedAt,
		&exp, &maxDL, &s.Downloads, &s.OneTime, &rev)
	if err != nil {
		return Share{}, err
	}
	if exp.Valid {
		s.ExpiresAt = &exp.Int64
	}
	if maxDL.Valid {
		s.MaxDownloads = &maxDL.Int64
	}
	if rev.Valid {
		s.RevokedAt = &rev.Int64
	}
	return s, nil
}

package store

import (
	"context"
	"database/sql"
	"errors"
)

// UpsertUploadSession creates or refreshes the tracking row for a tus upload.
func (db *DB) UpsertUploadSession(ctx context.Context, s UploadSession) error {
	if s.CreatedAt == 0 {
		s.CreatedAt = now()
	}
	s.UpdatedAt = now()
	_, err := db.ExecContext(ctx, `
		INSERT INTO upload_sessions(id,name,kind,mime,size,offset,status,file_id,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, kind=excluded.kind, mime=excluded.mime,
			size=excluded.size, offset=excluded.offset, status=excluded.status,
			file_id=excluded.file_id, updated_at=excluded.updated_at`,
		s.ID, s.Name, s.Kind, s.MIME, s.Size, s.Offset, s.Status, nullStr(s.FileID),
		s.CreatedAt, s.UpdatedAt)
	return err
}

// SetUploadProgress updates just the byte offset of an active upload.
func (db *DB) SetUploadProgress(ctx context.Context, id string, offset int64) error {
	_, err := db.ExecContext(ctx,
		`UPDATE upload_sessions SET offset = ?, updated_at = ? WHERE id = ? AND status = 'active'`,
		offset, now(), id)
	return err
}

// CompleteUploadSession marks an upload finished and links the created file.
func (db *DB) CompleteUploadSession(ctx context.Context, id, fileID string, size int64) error {
	_, err := db.ExecContext(ctx,
		`UPDATE upload_sessions SET status='completed', file_id=?, offset=?, size=?, updated_at=? WHERE id=?`,
		fileID, size, size, now(), id)
	return err
}

// FailUploadSession marks an upload failed.
func (db *DB) FailUploadSession(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE upload_sessions SET status='failed', updated_at=? WHERE id=?`, now(), id)
	return err
}

// GetUploadSession returns one tracking row.
func (db *DB) GetUploadSession(ctx context.Context, id string) (UploadSession, error) {
	var s UploadSession
	var fileID sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT id,name,kind,mime,size,offset,status,file_id,created_at,updated_at
		   FROM upload_sessions WHERE id = ?`, id).
		Scan(&s.ID, &s.Name, &s.Kind, &s.MIME, &s.Size, &s.Offset, &s.Status,
			&fileID, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return UploadSession{}, ErrNotFound
	}
	s.FileID = fileID.String
	return s, err
}

// ListUploadSessions returns recent upload sessions, newest first.
func (db *DB) ListUploadSessions(ctx context.Context, limit int) ([]UploadSession, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id,name,kind,mime,size,offset,status,file_id,created_at,updated_at
		   FROM upload_sessions ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UploadSession
	for rows.Next() {
		var s UploadSession
		var fileID sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.MIME, &s.Size, &s.Offset,
			&s.Status, &fileID, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.FileID = fileID.String
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteUploadSession removes a tracking row (used by the Transfers "remove"
// action and by cleanup).
func (db *DB) DeleteUploadSession(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM upload_sessions WHERE id = ?`, id)
	return err
}

// StaleUploadSessionIDs returns IDs of active uploads not touched since
// olderThan (unix seconds) — abandoned uploads eligible for cleanup.
func (db *DB) StaleUploadSessionIDs(ctx context.Context, olderThan int64) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM upload_sessions WHERE status = 'active' AND updated_at < ?`, olderThan)
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

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

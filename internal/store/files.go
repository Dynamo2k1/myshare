package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrNotFound is returned by lookups when no live row matches.
var ErrNotFound = errors.New("not found")

// FileListOptions controls ListFiles.
type FileListOptions struct {
	Kind   string // "", "file", "screenshot"
	Sort   string // name|size|created|updated (default created)
	Desc   bool
	Search string // simple LIKE over name
	Limit  int
	Cursor string // opaque; offset-encoded
}

// CreateFile inserts a file row and bumps the blob refcount in one transaction.
// The blob's bytes must already be on disk. Returns the created File.
func (db *DB) CreateFile(ctx context.Context, name, kind, mime string, size int64, hash string) (File, error) {
	if kind == "" {
		kind = "file"
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	f := File{
		ID: NewID(), Name: name, Kind: kind, MIME: mime,
		Size: size, Hash: hash, CreatedAt: now(), UpdatedAt: now(),
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := db.UpsertBlobRef(ctx, tx, hash, size); err != nil {
		return File{}, fmt.Errorf("blob ref: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO files(id,name,kind,mime,size,hash,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		f.ID, f.Name, f.Kind, f.MIME, f.Size, f.Hash, f.CreatedAt, f.UpdatedAt); err != nil {
		return File{}, err
	}
	if err := upsertFTS(ctx, tx, "file", f.ID, f.Name, ""); err != nil {
		return File{}, err
	}
	if err := tx.Commit(); err != nil {
		return File{}, err
	}
	return f, nil
}

// GetFile returns a single live file by ID.
func (db *DB) GetFile(ctx context.Context, id string) (File, error) {
	var f File
	err := db.QueryRowContext(ctx,
		`SELECT id,name,kind,mime,size,hash,created_at,updated_at
		   FROM files WHERE id = ? AND deleted_at IS NULL`, id).
		Scan(&f.ID, &f.Name, &f.Kind, &f.MIME, &f.Size, &f.Hash, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return f, err
}

// RenameFile updates a file's display name.
func (db *DB) RenameFile(ctx context.Context, id, name string) (File, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx,
		`UPDATE files SET name = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		name, now(), id)
	if err != nil {
		return File{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return File{}, ErrNotFound
	}
	if err := upsertFTS(ctx, tx, "file", id, name, ""); err != nil {
		return File{}, err
	}
	if err := tx.Commit(); err != nil {
		return File{}, err
	}
	return db.GetFile(ctx, id)
}

// DeleteFile soft-deletes the file row and releases its blob reference. It
// returns the blob hash and whether that blob is now orphaned (no more
// references), so the caller can remove the on-disk bytes.
func (db *DB) DeleteFile(ctx context.Context, id string) (hash string, orphaned bool, err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback() //nolint:errcheck

	err = tx.QueryRowContext(ctx,
		`SELECT hash FROM files WHERE id = ? AND deleted_at IS NULL`, id).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, ErrNotFound
	}
	if err != nil {
		return "", false, err
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE files SET deleted_at = ? WHERE id = ?`, now(), id); err != nil {
		return "", false, err
	}
	// Revoke any shares pointing at this file.
	if _, err = tx.ExecContext(ctx,
		`UPDATE shares SET revoked_at = ? WHERE file_id = ? AND revoked_at IS NULL`,
		now(), id); err != nil {
		return "", false, err
	}
	if _, err = tx.ExecContext(ctx,
		`DELETE FROM search_fts WHERE entity = 'file' AND ref_id = ?`, id); err != nil {
		return "", false, err
	}
	orphaned, err = db.ReleaseBlob(ctx, tx, hash)
	if err != nil {
		return "", false, err
	}
	if err = tx.Commit(); err != nil {
		return "", false, err
	}
	return hash, orphaned, nil
}

// ListFiles returns a page of live files.
func (db *DB) ListFiles(ctx context.Context, opt FileListOptions) (Page[File], error) {
	if opt.Limit <= 0 || opt.Limit > 200 {
		opt.Limit = 50
	}
	offset := 0
	if opt.Cursor != "" {
		if n, err := strconv.Atoi(opt.Cursor); err == nil && n >= 0 {
			offset = n
		}
	}

	where := []string{"deleted_at IS NULL"}
	args := []any{}
	if opt.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, opt.Kind)
	}
	if s := strings.TrimSpace(opt.Search); s != "" {
		where = append(where, "name LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeLike(s)+"%")
	}
	whereSQL := "WHERE " + strings.Join(where, " AND ")

	orderCol := map[string]string{
		"name": "name", "size": "size",
		"created": "created_at", "updated": "updated_at", "": "created_at",
	}[opt.Sort]
	if orderCol == "" {
		orderCol = "created_at"
	}
	dir := "ASC"
	if opt.Desc {
		dir = "DESC"
	}

	var page Page[File]
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM files "+whereSQL, args...).Scan(&page.Total); err != nil {
		return page, err
	}

	q := fmt.Sprintf(
		`SELECT id,name,kind,mime,size,hash,created_at,updated_at FROM files %s
		 ORDER BY %s %s, id %s LIMIT ? OFFSET ?`, whereSQL, orderCol, dir, dir)
	rows, err := db.QueryContext(ctx, q, append(args, opt.Limit, offset)...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.Name, &f.Kind, &f.MIME, &f.Size,
			&f.Hash, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return page, err
		}
		page.Items = append(page.Items, f)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if offset+len(page.Items) < int(page.Total) {
		page.NextCursor = strconv.Itoa(offset + len(page.Items))
	}
	return page, nil
}

// FileByHash returns any live file sharing the given blob hash (for
// duplicate-on-upload notification).
func (db *DB) FileByHash(ctx context.Context, hash string) (File, error) {
	var f File
	err := db.QueryRowContext(ctx,
		`SELECT id,name,kind,mime,size,hash,created_at,updated_at
		   FROM files WHERE hash = ? AND deleted_at IS NULL
		   ORDER BY created_at ASC LIMIT 1`, hash).
		Scan(&f.ID, &f.Name, &f.Kind, &f.MIME, &f.Size, &f.Hash, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return f, err
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

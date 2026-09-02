package store

import (
	"context"
	"database/sql"
	"errors"
)

// CreateClipboard adds a clipboard entry.
func (db *DB) CreateClipboard(ctx context.Context, content, format string) (ClipboardItem, error) {
	if format == "" {
		format = "text"
	}
	it := ClipboardItem{
		ID: NewID(), Content: content, Format: format,
		CreatedAt: now(), UpdatedAt: now(),
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ClipboardItem{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO clipboard(id,content,format,pinned,created_at,updated_at)
		 VALUES(?,?,?,0,?,?)`,
		it.ID, it.Content, it.Format, it.CreatedAt, it.UpdatedAt); err != nil {
		return ClipboardItem{}, err
	}
	if err := upsertFTS(ctx, tx, "clipboard", it.ID, "", content); err != nil {
		return ClipboardItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return ClipboardItem{}, err
	}
	return it, nil
}

// GetClipboard returns one entry.
func (db *DB) GetClipboard(ctx context.Context, id string) (ClipboardItem, error) {
	var it ClipboardItem
	err := db.QueryRowContext(ctx,
		`SELECT id,content,format,pinned,created_at,updated_at FROM clipboard WHERE id = ?`, id).
		Scan(&it.ID, &it.Content, &it.Format, &it.Pinned, &it.CreatedAt, &it.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ClipboardItem{}, ErrNotFound
	}
	return it, err
}

// UpdateClipboard patches content/format/pinned. Nil pointers are left as-is.
func (db *DB) UpdateClipboard(ctx context.Context, id string, content, format *string, pinned *bool) (ClipboardItem, error) {
	cur, err := db.GetClipboard(ctx, id)
	if err != nil {
		return ClipboardItem{}, err
	}
	if content != nil {
		cur.Content = *content
	}
	if format != nil {
		cur.Format = *format
	}
	if pinned != nil {
		cur.Pinned = *pinned
	}
	cur.UpdatedAt = now()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ClipboardItem{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`UPDATE clipboard SET content=?,format=?,pinned=?,updated_at=? WHERE id=?`,
		cur.Content, cur.Format, cur.Pinned, cur.UpdatedAt, id); err != nil {
		return ClipboardItem{}, err
	}
	if err := upsertFTS(ctx, tx, "clipboard", id, "", cur.Content); err != nil {
		return ClipboardItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return ClipboardItem{}, err
	}
	return cur, nil
}

// DeleteClipboard removes one entry.
func (db *DB) DeleteClipboard(ctx context.Context, id string) error {
	return db.deleteRow(ctx, "clipboard", "clipboard", id)
}

// ClearClipboard deletes every clipboard entry and its FTS rows.
func (db *DB) ClearClipboard(ctx context.Context) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `DELETE FROM clipboard`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_fts WHERE entity = 'clipboard'`); err != nil {
		return err
	}
	return tx.Commit()
}

// ListClipboard returns entries, pinned first then newest first.
func (db *DB) ListClipboard(ctx context.Context, search string, limit, offset int) (Page[ClipboardItem], error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where, args := "", []any{}
	if s := search; s != "" {
		where = `WHERE content LIKE ? ESCAPE '\'`
		args = append(args, "%"+escapeLike(s)+"%")
	}
	var page Page[ClipboardItem]
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM clipboard `+where, args...).
		Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id,content,format,pinned,created_at,updated_at FROM clipboard `+where+`
		 ORDER BY pinned DESC, created_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var it ClipboardItem
		if err := rows.Scan(&it.ID, &it.Content, &it.Format, &it.Pinned,
			&it.CreatedAt, &it.UpdatedAt); err != nil {
			return page, err
		}
		page.Items = append(page.Items, it)
	}
	if offset+len(page.Items) < int(page.Total) {
		page.NextCursor = itoa(offset + len(page.Items))
	}
	return page, rows.Err()
}

// deleteRow removes a row from table and its FTS entry for entity/id.
func (db *DB) deleteRow(ctx context.Context, table, entity, id string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM search_fts WHERE entity = ? AND ref_id = ?`, entity, id); err != nil {
		return err
	}
	return tx.Commit()
}

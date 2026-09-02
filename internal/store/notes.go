package store

import (
	"context"
	"database/sql"
	"errors"
)

// CreateNote adds a note.
func (db *DB) CreateNote(ctx context.Context, title, content string) (Note, error) {
	n := Note{ID: NewID(), Title: title, Content: content, CreatedAt: now(), UpdatedAt: now()}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Note{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO notes(id,title,content,pinned,created_at,updated_at) VALUES(?,?,?,0,?,?)`,
		n.ID, n.Title, n.Content, n.CreatedAt, n.UpdatedAt); err != nil {
		return Note{}, err
	}
	if err := upsertFTS(ctx, tx, "note", n.ID, n.Title, n.Content); err != nil {
		return Note{}, err
	}
	if err := tx.Commit(); err != nil {
		return Note{}, err
	}
	return n, nil
}

// GetNote returns one note.
func (db *DB) GetNote(ctx context.Context, id string) (Note, error) {
	var n Note
	err := db.QueryRowContext(ctx,
		`SELECT id,title,content,pinned,created_at,updated_at FROM notes WHERE id = ?`, id).
		Scan(&n.ID, &n.Title, &n.Content, &n.Pinned, &n.CreatedAt, &n.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, ErrNotFound
	}
	return n, err
}

// UpdateNote patches fields; nil pointers are unchanged.
func (db *DB) UpdateNote(ctx context.Context, id string, title, content *string, pinned *bool) (Note, error) {
	cur, err := db.GetNote(ctx, id)
	if err != nil {
		return Note{}, err
	}
	if title != nil {
		cur.Title = *title
	}
	if content != nil {
		cur.Content = *content
	}
	if pinned != nil {
		cur.Pinned = *pinned
	}
	cur.UpdatedAt = now()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Note{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`UPDATE notes SET title=?,content=?,pinned=?,updated_at=? WHERE id=?`,
		cur.Title, cur.Content, cur.Pinned, cur.UpdatedAt, id); err != nil {
		return Note{}, err
	}
	if err := upsertFTS(ctx, tx, "note", id, cur.Title, cur.Content); err != nil {
		return Note{}, err
	}
	if err := tx.Commit(); err != nil {
		return Note{}, err
	}
	return cur, nil
}

// DeleteNote removes one note.
func (db *DB) DeleteNote(ctx context.Context, id string) error {
	return db.deleteRow(ctx, "notes", "note", id)
}

// ListNotes returns notes, pinned first then most-recently-updated.
func (db *DB) ListNotes(ctx context.Context, search string, limit, offset int) (Page[Note], error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where, args := "", []any{}
	if search != "" {
		where = `WHERE title LIKE ? ESCAPE '\' OR content LIKE ? ESCAPE '\'`
		like := "%" + escapeLike(search) + "%"
		args = append(args, like, like)
	}
	var page Page[Note]
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes `+where, args...).
		Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id,title,content,pinned,created_at,updated_at FROM notes `+where+`
		 ORDER BY pinned DESC, updated_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.Pinned,
			&n.CreatedAt, &n.UpdatedAt); err != nil {
			return page, err
		}
		page.Items = append(page.Items, n)
	}
	if offset+len(page.Items) < int(page.Total) {
		page.NextCursor = itoa(offset + len(page.Items))
	}
	return page, rows.Err()
}

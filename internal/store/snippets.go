package store

import (
	"context"
	"database/sql"
	"errors"
)

// CreateSnippet adds a snippet.
func (db *DB) CreateSnippet(ctx context.Context, title, content, language string) (Snippet, error) {
	if language == "" {
		language = "plaintext"
	}
	s := Snippet{
		ID: NewID(), Title: title, Content: content, Language: language,
		CreatedAt: now(), UpdatedAt: now(),
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Snippet{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO snippets(id,title,content,language,pinned,created_at,updated_at)
		 VALUES(?,?,?,?,0,?,?)`,
		s.ID, s.Title, s.Content, s.Language, s.CreatedAt, s.UpdatedAt); err != nil {
		return Snippet{}, err
	}
	if err := upsertFTS(ctx, tx, "snippet", s.ID, s.Title, s.Content); err != nil {
		return Snippet{}, err
	}
	if err := tx.Commit(); err != nil {
		return Snippet{}, err
	}
	return s, nil
}

// GetSnippet returns one snippet.
func (db *DB) GetSnippet(ctx context.Context, id string) (Snippet, error) {
	var s Snippet
	err := db.QueryRowContext(ctx,
		`SELECT id,title,content,language,pinned,created_at,updated_at FROM snippets WHERE id = ?`, id).
		Scan(&s.ID, &s.Title, &s.Content, &s.Language, &s.Pinned, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Snippet{}, ErrNotFound
	}
	return s, err
}

// UpdateSnippet patches fields; nil pointers are unchanged.
func (db *DB) UpdateSnippet(ctx context.Context, id string, title, content, language *string, pinned *bool) (Snippet, error) {
	cur, err := db.GetSnippet(ctx, id)
	if err != nil {
		return Snippet{}, err
	}
	if title != nil {
		cur.Title = *title
	}
	if content != nil {
		cur.Content = *content
	}
	if language != nil {
		cur.Language = *language
	}
	if pinned != nil {
		cur.Pinned = *pinned
	}
	cur.UpdatedAt = now()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Snippet{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`UPDATE snippets SET title=?,content=?,language=?,pinned=?,updated_at=? WHERE id=?`,
		cur.Title, cur.Content, cur.Language, cur.Pinned, cur.UpdatedAt, id); err != nil {
		return Snippet{}, err
	}
	if err := upsertFTS(ctx, tx, "snippet", id, cur.Title, cur.Content); err != nil {
		return Snippet{}, err
	}
	if err := tx.Commit(); err != nil {
		return Snippet{}, err
	}
	return cur, nil
}

// DeleteSnippet removes one snippet.
func (db *DB) DeleteSnippet(ctx context.Context, id string) error {
	return db.deleteRow(ctx, "snippets", "snippet", id)
}

// ListSnippets returns snippets, pinned first then most-recently-updated.
func (db *DB) ListSnippets(ctx context.Context, search string, limit, offset int) (Page[Snippet], error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where, args := "", []any{}
	if search != "" {
		where = `WHERE title LIKE ? ESCAPE '\' OR content LIKE ? ESCAPE '\'`
		like := "%" + escapeLike(search) + "%"
		args = append(args, like, like)
	}
	var page Page[Snippet]
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM snippets `+where, args...).
		Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id,title,content,language,pinned,created_at,updated_at FROM snippets `+where+`
		 ORDER BY pinned DESC, updated_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var s Snippet
		if err := rows.Scan(&s.ID, &s.Title, &s.Content, &s.Language, &s.Pinned,
			&s.CreatedAt, &s.UpdatedAt); err != nil {
			return page, err
		}
		page.Items = append(page.Items, s)
	}
	if offset+len(page.Items) < int(page.Total) {
		page.NextCursor = itoa(offset + len(page.Items))
	}
	return page, rows.Err()
}

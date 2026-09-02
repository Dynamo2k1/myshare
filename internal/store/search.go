package store

import (
	"context"
	"database/sql"
	"strings"
)

// upsertFTS keeps the search_fts row for (entity, refID) current. It is called
// inside the same transaction as the entity write so the index never drifts.
func upsertFTS(ctx context.Context, tx *sql.Tx, entity, refID, title, body string) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM search_fts WHERE entity = ? AND ref_id = ?`, entity, refID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO search_fts(entity, ref_id, title, body) VALUES(?,?,?,?)`,
		entity, refID, title, body)
	return err
}

// Search runs a global full-text query across files, clipboard, snippets and
// notes. It never touches blob contents — only indexed metadata and text.
func (db *DB) Search(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}

	rows, err := db.QueryContext(ctx, `
		SELECT entity, ref_id, title,
		       snippet(search_fts, 3, '[', ']', ' … ', 12) AS snip
		  FROM search_fts
		 WHERE search_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`, ftsQuery(q), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.Entity, &h.RefID, &h.Title, &h.Snippet); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// ftsQuery turns free user text into a safe FTS5 MATCH expression: each
// whitespace-separated term is stripped of characters with special meaning to
// FTS5, wrapped in double quotes, and given a prefix (*) match. Terms that are
// empty after stripping are dropped. If nothing usable remains, a token that
// cannot match anything is returned so the caller still runs a valid — but
// empty — query instead of erroring.
func ftsQuery(s string) string {
	var terms []string
	for _, f := range strings.Fields(s) {
		f = strings.Map(func(r rune) rune {
			switch r {
			case '"', '(', ')', '*', ':', '^', '-':
				return -1
			}
			return r
		}, f)
		if f == "" {
			continue
		}
		terms = append(terms, `"`+f+`"*`)
	}
	if len(terms) == 0 {
		return `"` + "\x00nomatch\x00" + `"`
	}
	return strings.Join(terms, " ")
}

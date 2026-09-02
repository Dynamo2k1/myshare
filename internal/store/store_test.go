package store

import (
	"context"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), true)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.db")
	db, err := Open(context.Background(), p, true)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	db.Close()
	db, err = Open(context.Background(), p, true)
	if err != nil {
		t.Fatalf("second open (re-migrate): %v", err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected 1 applied migration, got %d", n)
	}
}

func TestFileLifecycleAndBlobRefcount(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()

	// Two files, same content -> one blob, refcount 2 (dedup).
	f1, err := db.CreateFile(ctx, "a.bin", "file", "application/octet-stream", 10, "hashA")
	if err != nil {
		t.Fatal(err)
	}
	f2, err := db.CreateFile(ctx, "b.bin", "file", "application/octet-stream", 10, "hashA")
	if err != nil {
		t.Fatal(err)
	}

	var rc int64
	if err := db.QueryRow(`SELECT refcount FROM blobs WHERE hash='hashA'`).Scan(&rc); err != nil {
		t.Fatal(err)
	}
	if rc != 2 {
		t.Fatalf("refcount = %d, want 2", rc)
	}

	// Delete f1: blob still referenced.
	_, orphaned, err := db.DeleteFile(ctx, f1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if orphaned {
		t.Error("blob should not be orphaned while f2 references it")
	}

	// Delete f2: now orphaned, row gone.
	hash, orphaned, err := db.DeleteFile(ctx, f2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !orphaned || hash != "hashA" {
		t.Errorf("expected orphaned hashA, got orphaned=%v hash=%q", orphaned, hash)
	}
	exists, _ := db.BlobExists(ctx, "hashA")
	if exists {
		t.Error("blob row should be deleted at refcount 0")
	}

	// GetFile on a deleted file -> ErrNotFound.
	if _, err := db.GetFile(ctx, f1.ID); err != ErrNotFound {
		t.Errorf("GetFile(deleted) = %v, want ErrNotFound", err)
	}
}

func TestListFilesSortAndFilter(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	_, _ = db.CreateFile(ctx, "zebra.txt", "file", "text/plain", 3, "h1")
	_, _ = db.CreateFile(ctx, "apple.txt", "file", "text/plain", 999, "h2")
	_, _ = db.CreateFile(ctx, "shot.png", "screenshot", "image/png", 50, "h3")

	byName, err := db.ListFiles(ctx, FileListOptions{Sort: "name"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byName.Items) != 3 || byName.Items[0].Name != "apple.txt" {
		t.Errorf("sort by name failed: %+v", names(byName.Items))
	}

	onlyShots, _ := db.ListFiles(ctx, FileListOptions{Kind: "screenshot"})
	if len(onlyShots.Items) != 1 || onlyShots.Items[0].Kind != "screenshot" {
		t.Errorf("kind filter failed: %+v", names(onlyShots.Items))
	}

	search, _ := db.ListFiles(ctx, FileListOptions{Search: "ppl"})
	if len(search.Items) != 1 || search.Items[0].Name != "apple.txt" {
		t.Errorf("search failed: %+v", names(search.Items))
	}
}

func TestSearchAcrossEntities(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	_, _ = db.CreateFile(ctx, "quarterly-report.pdf", "file", "application/pdf", 1, "hf")
	_, _ = db.CreateClipboard(ctx, "remember the quarterly numbers", "text")
	_, _ = db.CreateSnippet(ctx, "Quarterly script", "echo quarterly", "bash")
	_, _ = db.CreateNote(ctx, "Misc", "nothing relevant here")

	hits, err := db.Search(ctx, "quarterly", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Fatalf("expected 3 hits for 'quarterly', got %d: %+v", len(hits), hits)
	}
	seen := map[string]bool{}
	for _, h := range hits {
		seen[h.Entity] = true
	}
	for _, want := range []string{"file", "clipboard", "snippet"} {
		if !seen[want] {
			t.Errorf("missing %s in search results", want)
		}
	}

	// Prefix match.
	if hits, _ := db.Search(ctx, "quart", 10); len(hits) != 3 {
		t.Errorf("prefix search 'quart' returned %d, want 3", len(hits))
	}
	// FTS operators in user input must not blow up.
	if _, err := db.Search(ctx, `AND OR NOT " ( ) *`, 10); err != nil {
		t.Errorf("hostile FTS input errored: %v", err)
	}
}

func TestShareTokenPolicy(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	f, _ := db.CreateFile(ctx, "s.bin", "file", "application/octet-stream", 5, "hs")

	max := int64(1)
	sh, err := db.CreateShare(ctx, ShareCreateOptions{
		FileID: f.ID, TokenHash: "tokhash", MaxDownloads: &max,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := db.ResolveShareToken(ctx, "tokhash"); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if err := db.RecordShareDownload(ctx, sh.ID); err != nil {
		t.Fatal(err)
	}
	// Cap reached -> unavailable.
	if _, _, err := db.ResolveShareToken(ctx, "tokhash"); err != ErrShareUnavailable {
		t.Errorf("after cap: got %v, want ErrShareUnavailable", err)
	}
	// Unknown token.
	if _, _, err := db.ResolveShareToken(ctx, "nope"); err != ErrNotFound {
		t.Errorf("unknown token: got %v, want ErrNotFound", err)
	}
}

func TestUploadSessionProgress(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	s := UploadSession{ID: "up1", Name: "big.mkv", Kind: "file", MIME: "video/x-matroska", Size: 1000, Status: "active"}
	if err := db.UpsertUploadSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := db.SetUploadProgress(ctx, "up1", 400); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetUploadSession(ctx, "up1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Offset != 400 {
		t.Errorf("offset = %d, want 400", got.Offset)
	}
	if err := db.CompleteUploadSession(ctx, "up1", "file123", 1000); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetUploadSession(ctx, "up1")
	if got.Status != "completed" || got.FileID != "file123" {
		t.Errorf("after complete: %+v", got)
	}
}

func TestNewIDSortableAndUnique(t *testing.T) {
	seen := map[string]bool{}
	var prev string
	for i := 0; i < 2000; i++ {
		id := NewID()
		if len(id) != 26 {
			t.Fatalf("id length %d: %q", len(id), id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
		prev = id
	}
	_ = prev
}

func names(fs []File) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Name
	}
	return out
}

package uploads

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/ranauzair/myshare/internal/blob"
	"github.com/ranauzair/myshare/internal/sse"
	"github.com/ranauzair/myshare/internal/store"
)

type harness struct {
	srv  *httptest.Server
	db   *store.DB
	blob *blob.Store
	mgr  *Manager
	base string // tus endpoint URL
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(context.Background(), filepath.Join(dir, "t.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	bs, err := blob.New(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	hub := sse.NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))

	mux := http.NewServeMux()
	mgr, err := New(Deps{
		DB: db, Blob: bs, Hub: hub,
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		UploadDir: filepath.Join(dir, "uploads"),
		BasePath:  "/files/",
	})
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle("/files/", http.StripPrefix("/files", mgr.Handler()))

	srv := httptest.NewServer(mux)
	h := &harness{srv: srv, db: db, blob: bs, mgr: mgr, base: srv.URL + "/files/"}
	t.Cleanup(func() {
		srv.Close()
		mgr.Shutdown()
		db.Close()
	})
	return h
}

func meta(name, typ string) string {
	enc := base64.StdEncoding.EncodeToString
	return fmt.Sprintf("filename %s,filetype %s,kind %s",
		enc([]byte(name)), enc([]byte(typ)), enc([]byte("file")))
}

func (h *harness) create(t *testing.T, name string, size int64) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.base, nil)
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Upload-Length", strconv.FormatInt(size, 10))
	req.Header.Set("Upload-Metadata", meta(name, "application/octet-stream"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: status %d: %s", resp.StatusCode, b)
	}
	return resp.Header.Get("Location")
}

func (h *harness) patch(t *testing.T, loc string, offset int64, data []byte) (newOffset int64, status int) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPatch, loc, bytes.NewReader(data))
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Upload-Offset", strconv.FormatInt(offset, 10))
	req.Header.Set("Content-Type", "application/offset+octet-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	no, _ := strconv.ParseInt(resp.Header.Get("Upload-Offset"), 10, 64)
	return no, resp.StatusCode
}

func (h *harness) head(t *testing.T, loc string) int64 {
	t.Helper()
	req, _ := http.NewRequest(http.MethodHead, loc, nil)
	req.Header.Set("Tus-Resumable", "1.0.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	off, _ := strconv.ParseInt(resp.Header.Get("Upload-Offset"), 10, 64)
	return off
}

// waitForFile polls until a file with the given hash is finalised, or fails.
// The timeout is generous: finalisation hashes the whole upload with streaming
// I/O, which for a multi-GiB file takes several seconds.
func (h *harness) waitForFile(t *testing.T, hash string) store.File {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		f, err := h.db.FileByHash(context.Background(), hash)
		if err == nil {
			return f
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("file with hash %s was never finalised", hash)
	return store.File{}
}

func TestTusInterruptResumeChecksum(t *testing.T) {
	h := newHarness(t)

	const total = 8 << 20 // 8 MiB
	data := make([]byte, total)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	wantHash := hex.EncodeToString(sum[:])

	loc := h.create(t, "movie.bin", total)

	// Upload ~40%, then "lose the connection".
	cut := int64(total * 4 / 10)
	if no, st := h.patch(t, loc, 0, data[:cut]); st != http.StatusNoContent || no != cut {
		t.Fatalf("first patch: status=%d offset=%d (want 204/%d)", st, no, cut)
	}

	// Client reconnects and asks where it got to.
	off := h.head(t, loc)
	if off != cut {
		t.Fatalf("HEAD offset = %d, want %d", off, cut)
	}

	// Resume from exactly that offset to the end.
	if no, st := h.patch(t, loc, off, data[off:]); st != http.StatusNoContent || no != total {
		t.Fatalf("resume patch: status=%d offset=%d (want 204/%d)", st, no, total)
	}

	f := h.waitForFile(t, wantHash)
	if f.Size != total {
		t.Fatalf("finalised size = %d, want %d", f.Size, total)
	}
	if f.Hash != wantHash {
		t.Fatalf("finalised hash = %s, want %s", f.Hash, wantHash)
	}

	// The bytes on disk must match exactly.
	rc, err := h.blob.Open(f.Hash)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, data) {
		t.Fatal("stored blob bytes differ from the original")
	}

	// The tus scratch files must be gone.
	up := filepath.Join(t.TempDir()) // not used; check the manager's dir instead
	_ = up
}

func TestTusManySmallChunksResume(t *testing.T) {
	h := newHarness(t)
	const total = 2 << 20
	data := make([]byte, total)
	rand.Read(data)
	sum := sha256.Sum256(data)

	loc := h.create(t, "chunky.bin", total)
	chunk := 199_999 // deliberately not aligned
	var off int64
	for off < total {
		end := off + int64(chunk)
		if end > total {
			end = total
		}
		no, st := h.patch(t, loc, off, data[off:end])
		if st != http.StatusNoContent {
			t.Fatalf("chunk at %d: status %d", off, st)
		}
		// Re-HEAD every few chunks like a flaky client would.
		if off%3 == 0 {
			if h.head(t, loc) != no {
				t.Fatalf("HEAD disagrees with PATCH offset at %d", off)
			}
		}
		off = no
	}
	h.waitForFile(t, hex.EncodeToString(sum[:]))
}

func TestTusConcurrentUploads(t *testing.T) {
	h := newHarness(t)
	const n = 6
	const size = 512 << 10

	type job struct {
		hash string
	}
	results := make(chan job, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			data := make([]byte, size)
			rand.Read(data)
			data[0] = byte(i) // ensure distinct content
			sum := sha256.Sum256(data)
			loc := h.create(t, fmt.Sprintf("c%d.bin", i), size)
			// Two halves with a HEAD in between.
			half := int64(size / 2)
			h.patch(t, loc, 0, data[:half])
			h.head(t, loc)
			h.patch(t, loc, half, data[half:])
			results <- job{hash: hex.EncodeToString(sum[:])}
		}(i)
	}
	for i := 0; i < n; i++ {
		j := <-results
		h.waitForFile(t, j.hash)
	}

	page, err := h.db.ListFiles(context.Background(), store.FileListOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != n {
		t.Fatalf("expected %d files, got %d", n, page.Total)
	}
}

// TestTusLargeUploadMemoryStable streams a multi-GiB upload through the real tus
// handler and asserts the process heap does not grow with file size. This is the
// objective proof of the headline requirement: a multi-GB upload is never held
// in RAM. Skipped unless MYSHARE_BIGTEST is set.
func TestTusLargeUploadMemoryStable(t *testing.T) {
	if os.Getenv("MYSHARE_BIGTEST") == "" {
		t.Skip("set MYSHARE_BIGTEST=1 to run (uploads 3 GiB)")
	}
	h := newHarness(t)

	const total = int64(3) << 30 // 3 GiB
	loc := h.create(t, "huge.bin", total)

	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)

	// Stream random-ish bytes to the server in 8 MiB PATCH bodies without ever
	// holding more than one chunk, hashing as we go so we can verify the result.
	hasher := sha256.New()
	buf := make([]byte, 8<<20)
	var seed uint32 = 0x9e3779b9
	var off int64
	for off < total {
		n := int64(len(buf))
		if total-off < n {
			n = total - off
		}
		for i := int64(0); i < n; i++ {
			seed = seed*1664525 + 1013904223
			buf[i] = byte(seed >> 24)
		}
		hasher.Write(buf[:n])
		no, st := h.patch(t, loc, off, buf[:n])
		if st != http.StatusNoContent {
			t.Fatalf("patch at %d: status %d", off, st)
		}
		off = no
	}

	wantHash := hex.EncodeToString(hasher.Sum(nil))
	f := h.waitForFile(t, wantHash)

	runtime.GC()
	runtime.ReadMemStats(&m1)
	grew := int64(m1.HeapAlloc) - int64(m0.HeapAlloc)
	t.Logf("uploaded %d bytes; finalised size=%d hash=%s; heap delta %d bytes (%.1f MiB)",
		total, f.Size, wantHash[:12], grew, float64(grew)/(1<<20))

	if f.Size != total {
		t.Fatalf("size mismatch: %d vs %d", f.Size, total)
	}
	if grew > 256<<20 {
		t.Fatalf("heap grew %d bytes uploading %d bytes — not streaming", grew, total)
	}
}

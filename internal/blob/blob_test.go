package blob

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFromAndOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("hello myshare")
	want := sha256.Sum256(data)
	wantHex := hex.EncodeToString(want[:])

	hash, n, existed, err := s.WriteFrom(bytes.NewReader(data), filepath.Join(dir, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if hash != wantHex || n != int64(len(data)) || existed {
		t.Fatalf("got hash=%s n=%d existed=%v", hash, n, existed)
	}
	if !s.Has(hash) {
		t.Fatal("Has() false after write")
	}

	f, err := s.Open(hash)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, _ := io.ReadAll(f)
	if !bytes.Equal(got, data) {
		t.Fatalf("round-trip mismatch: %q", got)
	}
}

func TestWriteFromDedup(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(filepath.Join(dir, "blobs"))
	data := bytes.Repeat([]byte("x"), 4096)

	h1, _, e1, err := s.WriteFrom(bytes.NewReader(data), "")
	if err != nil || e1 {
		t.Fatalf("first write: h=%s existed=%v err=%v", h1, e1, err)
	}
	h2, _, e2, err := s.WriteFrom(bytes.NewReader(data), "")
	if err != nil {
		t.Fatal(err)
	}
	if h2 != h1 || !e2 {
		t.Fatalf("second write should dedupe: h=%s existed=%v", h2, e2)
	}
}

func TestAdoptRename(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(filepath.Join(dir, "blobs"))

	src := filepath.Join(dir, "upload.bin")
	data := []byte("adopt me")
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	existed, err := s.Adopt(src, hash)
	if err != nil || existed {
		t.Fatalf("adopt: existed=%v err=%v", existed, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source should be gone after adopt-by-rename")
	}
	if !s.Has(hash) {
		t.Error("blob missing after adopt")
	}

	// Adopting a duplicate removes the source and reports existed.
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	existed, err = s.Adopt(src, hash)
	if err != nil || !existed {
		t.Fatalf("adopt dup: existed=%v err=%v", existed, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("duplicate source should be removed")
	}
}

func TestRemovePrunesDirs(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(filepath.Join(dir, "blobs"))
	hash, _, _, _ := s.WriteFrom(strings.NewReader("prune"), "")
	if err := s.Remove(hash); err != nil {
		t.Fatal(err)
	}
	if s.Has(hash) {
		t.Error("blob still present after Remove")
	}
	// Removing again is not an error.
	if err := s.Remove(hash); err != nil {
		t.Errorf("second Remove errored: %v", err)
	}
}

func TestInvalidHashRejected(t *testing.T) {
	s, _ := New(filepath.Join(t.TempDir(), "blobs"))
	for _, bad := range []string{"", "xyz", strings.Repeat("g", 64), "../../etc/passwd", strings.Repeat("a", 63)} {
		if _, err := s.Open(bad); err == nil {
			t.Errorf("Open(%q) should have failed", bad)
		}
		if s.Has(bad) {
			t.Errorf("Has(%q) should be false", bad)
		}
	}
}

// TestLargeStreamMemoryStable proves the store does not buffer whole files:
// hashing a large reader must not grow the heap materially. Skipped unless
// MYSHARE_BIGTEST is set (it allocates a multi-hundred-MB sparse stream).
func TestLargeStreamMemoryStable(t *testing.T) {
	if os.Getenv("MYSHARE_BIGTEST") == "" {
		t.Skip("set MYSHARE_BIGTEST=1 to run")
	}
	const size = int64(2) << 30 // 2 GiB

	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)

	r := io.LimitReader(zeroReader{}, size)
	hash, n, err := HashReader(r)
	if err != nil || n != size {
		t.Fatalf("hash: n=%d err=%v", n, err)
	}

	runtime.GC()
	runtime.ReadMemStats(&m1)
	grew := int64(m1.HeapAlloc) - int64(m0.HeapAlloc)
	t.Logf("hashed %d bytes -> %s; heap delta %d bytes", n, hash, grew)
	if grew > 64<<20 {
		t.Fatalf("heap grew %d bytes hashing %d bytes — not streaming", grew, size)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

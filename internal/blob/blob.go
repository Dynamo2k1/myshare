// Package blob stores file contents on disk, addressed by the SHA-256 of the
// bytes. Nothing in this package ever holds a whole file in memory: hashing and
// copying are streamed with a fixed buffer.
package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// copyBufSize is the streaming buffer used for hashing and copying. 1 MiB keeps
// syscall overhead low while keeping the resident set flat regardless of file
// size.
const copyBufSize = 1 << 20

// Store is a content-addressed blob directory.
type Store struct {
	root string // e.g. <dataDir>/blobs
}

// New returns a Store rooted at dir, creating it if necessary.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create blob dir: %w", err)
	}
	return &Store{root: filepath.Clean(dir)}, nil
}

// Root returns the blob directory.
func (s *Store) Root() string { return s.root }

// pathFor returns the on-disk path for a hash: <root>/ab/cd/<hash>. The
// two-level fan-out keeps directory sizes manageable at scale.
func (s *Store) pathFor(hash string) string {
	return filepath.Join(s.root, hash[0:2], hash[2:4], hash)
}

// Has reports whether a blob with the given hash is present on disk.
func (s *Store) Has(hash string) bool {
	if !validHash(hash) {
		return false
	}
	st, err := os.Stat(s.pathFor(hash))
	return err == nil && !st.IsDir()
}

// HashReader streams r through SHA-256 and returns the lowercase hex digest and
// the number of bytes read. No data is retained.
func HashReader(r io.Reader) (hash string, n int64, err error) {
	h := sha256.New()
	buf := make([]byte, copyBufSize)
	n, err = io.CopyBuffer(h, r, buf)
	if err != nil {
		return "", n, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// HashFile streams a file on disk through SHA-256.
func (s *Store) HashFile(path string) (hash string, n int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	return HashReader(f)
}

// Adopt moves an already-complete file at srcPath into the store under the given
// hash. The caller is expected to have hashed srcPath already (typically the
// finished tus upload). If a blob with that hash already exists, srcPath is
// removed and existed is true (deduplication). The move is a rename when srcPath
// is on the same filesystem as the store (the normal case), else a streamed
// copy followed by removal.
func (s *Store) Adopt(srcPath, hash string) (existed bool, err error) {
	if !validHash(hash) {
		return false, fmt.Errorf("blob: invalid hash %q", hash)
	}
	dst := s.pathFor(hash)

	if s.Has(hash) {
		_ = os.Remove(srcPath)
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}

	// Fast path: atomic rename. This succeeds when srcPath and the store share a
	// filesystem, which is the normal case (uploads/ and blobs/ under one data
	// dir). It is instant and constant-memory regardless of file size.
	if err := os.Rename(srcPath, dst); err == nil {
		return false, nil
	} else if os.IsExist(err) || s.Has(hash) {
		// Lost a race to an identical blob: dedupe.
		_ = os.Remove(srcPath)
		return true, nil
	}

	// Rename failed (cross-device, or Windows volume boundary). Fall back to a
	// streamed copy into place, then drop the source.
	if err := s.copyIntoPlace(srcPath, dst); err != nil {
		return false, err
	}
	_ = os.Remove(srcPath)
	return false, nil
}

func (s *Store) copyIntoPlace(srcPath, dst string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".blob-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	buf := make([]byte, copyBufSize)
	if _, err := io.CopyBuffer(tmp, src, buf); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		if os.IsExist(err) || s.Has(filepath.Base(dst)) {
			return nil // someone else won the race; that's fine
		}
		return err
	}
	return nil
}

// WriteFrom streams r into the store, hashing as it goes, and returns the hash
// and byte count. Used for small direct uploads that never touch tus. The data
// is written to a temp file first, then adopted under its hash.
func (s *Store) WriteFrom(r io.Reader, tmpDir string) (hash string, n int64, existed bool, err error) {
	if tmpDir == "" {
		tmpDir = s.root
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", 0, false, err
	}
	tmp, err := os.CreateTemp(tmpDir, "up-*")
	if err != nil {
		return "", 0, false, err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	h := sha256.New()
	buf := make([]byte, copyBufSize)
	n, err = io.CopyBuffer(io.MultiWriter(tmp, h), r, buf)
	if err != nil {
		tmp.Close()
		return "", n, false, err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return "", n, false, err
	}
	if err = tmp.Close(); err != nil {
		return "", n, false, err
	}
	hash = hex.EncodeToString(h.Sum(nil))
	existed, err = s.Adopt(tmpName, hash)
	return hash, n, existed, err
}

// Open returns a read-only handle to a blob's bytes. The caller closes it.
func (s *Store) Open(hash string) (*os.File, error) {
	if !validHash(hash) {
		return nil, fmt.Errorf("blob: invalid hash %q", hash)
	}
	return os.Open(s.pathFor(hash))
}

// Remove deletes a blob's bytes. Missing is not an error.
func (s *Store) Remove(hash string) error {
	if !validHash(hash) {
		return fmt.Errorf("blob: invalid hash %q", hash)
	}
	err := os.Remove(s.pathFor(hash))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Best-effort prune of now-empty fan-out dirs.
	_ = os.Remove(filepath.Dir(s.pathFor(hash)))
	_ = os.Remove(filepath.Dir(filepath.Dir(s.pathFor(hash))))
	return nil
}

// Size returns a blob's size in bytes.
func (s *Store) Size(hash string) (int64, error) {
	st, err := os.Stat(s.pathFor(hash))
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

func validHash(h string) bool {
	if len(h) != 64 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

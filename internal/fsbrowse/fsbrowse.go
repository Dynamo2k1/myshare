// Package fsbrowse is a filesystem-backed file browser rooted at a single
// directory. It is what powers MyShare's "directory mode": the Files tab shows
// the real contents of a folder on disk, uploads write real files into it, and
// deletes remove them.
//
// Every operation takes a client-supplied relative path and resolves it through
// safepath so nothing can escape the root — symlinks included.
package fsbrowse

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dynamo2k1/myshare/internal/safepath"
)

// metaDirName is MyShare's own hidden state folder inside the served directory.
// It is hidden from listings and refused as a navigation target.
const metaDirName = ".myshare"

// ErrNotFound is returned when a path does not resolve to an existing entry.
var ErrNotFound = errors.New("not found")

// Browser serves a directory tree.
type Browser struct {
	root string // absolute, cleaned
}

// Entry is one item in a directory listing.
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"` // slash-separated, relative to root, no leading slash
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	MIME    string `json:"mime,omitempty"`
	ModTime int64  `json:"mod_time"`
}

// Listing is the result of List: the entries plus breadcrumb context.
type Listing struct {
	Dir     string  `json:"dir"`    // the relative dir that was listed ("" = root)
	Parent  string  `json:"parent"` // relative parent dir, or "" at root
	Entries []Entry `json:"entries"`
}

// New returns a Browser rooted at dir (which must already exist and be a
// directory) and ensures the hidden meta directory exists.
func New(dir string) (*Browser, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	abs = filepath.Clean(abs)
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("fsbrowse: %s is not a directory", abs)
	}
	return &Browser{root: abs}, nil
}

// Root returns the absolute served directory.
func (b *Browser) Root() string { return b.root }

// MetaDir returns <root>/.myshare, creating it. Used when MyShare keeps its
// database alongside the served files (non-ephemeral directory mode).
func (b *Browser) MetaDir() (string, error) {
	p := filepath.Join(b.root, metaDirName)
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", err
	}
	return p, nil
}

// resolve turns an untrusted slash-path into an absolute path guaranteed to be
// inside root. An empty or "." path resolves to the root itself. Any "..", any
// absolute path, or anything under the hidden meta dir is refused outright.
func (b *Browser) resolve(raw string) (string, error) {
	raw = strings.ReplaceAll(raw, "\\", "/")
	for _, seg := range strings.Split(raw, "/") {
		if seg == ".." {
			return "", safepath.ErrUnsafePath
		}
	}
	rel := strings.TrimPrefix(path.Clean("/"+raw), "/")
	if rel == "" || rel == "." {
		if raw != "" && raw != "." && raw != "/" {
			return "", safepath.ErrUnsafePath
		}
		return b.root, nil
	}
	abs, err := safepath.ResolveInside(b.root, filepath.FromSlash(rel))
	if err != nil {
		return "", err
	}
	// Reject anything under the hidden meta dir.
	first := strings.SplitN(rel, "/", 2)[0]
	if first == metaDirName {
		return "", safepath.ErrUnsafePath
	}
	// Defence against symlink escapes: the real path must still be inside root.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		if !safepath.WithinBase(b.root, real) {
			return "", safepath.ErrUnsafePath
		}
	}
	return abs, nil
}

func (b *Browser) rel(abs string) string {
	r, err := filepath.Rel(b.root, abs)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(r)
}

// List returns the entries of the directory at relDir.
func (b *Browser) List(relDir string) (Listing, error) {
	abs, err := b.resolve(relDir)
	if err != nil {
		return Listing{}, err
	}
	des, err := os.ReadDir(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return Listing{}, ErrNotFound
		}
		return Listing{}, err
	}

	cleanRel := b.rel(abs)
	if cleanRel == "." {
		cleanRel = ""
	}
	l := Listing{Dir: cleanRel, Parent: parentOf(cleanRel)}

	for _, de := range des {
		name := de.Name()
		if name == metaDirName || strings.HasPrefix(name, ".myshare") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		// A symlink that points outside root is skipped entirely.
		full := filepath.Join(abs, name)
		if info.Mode()&os.ModeSymlink != 0 {
			if real, err := filepath.EvalSymlinks(full); err != nil ||
				!safepath.WithinBase(b.root, real) {
				continue
			}
			if rst, err := os.Stat(full); err == nil {
				info = rst
			}
		}
		e := Entry{
			Name:    name,
			Path:    joinRel(cleanRel, name),
			IsDir:   info.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		}
		if !e.IsDir {
			if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
				e.MIME = t
			}
		}
		l.Entries = append(l.Entries, e)
	}

	sort.Slice(l.Entries, func(i, j int) bool {
		if l.Entries[i].IsDir != l.Entries[j].IsDir {
			return l.Entries[i].IsDir // folders first
		}
		return strings.ToLower(l.Entries[i].Name) < strings.ToLower(l.Entries[j].Name)
	})
	return l, nil
}

// Stat returns file info for a single path.
func (b *Browser) Stat(rel string) (Entry, string, error) {
	abs, err := b.resolve(rel)
	if err != nil {
		return Entry{}, "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return Entry{}, "", ErrNotFound
		}
		return Entry{}, "", err
	}
	e := Entry{
		Name: info.Name(), Path: b.rel(abs), IsDir: info.IsDir(),
		Size: info.Size(), ModTime: info.ModTime().Unix(),
	}
	if !e.IsDir {
		if t := mime.TypeByExtension(filepath.Ext(e.Name)); t != "" {
			e.MIME = t
		}
	}
	return e, abs, nil
}

// Open returns a readable handle to a file for streaming downloads/previews.
func (b *Browser) Open(rel string) (*os.File, os.FileInfo, error) {
	abs, err := b.resolve(rel)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if info.IsDir() {
		f.Close()
		return nil, nil, fmt.Errorf("%s is a directory", rel)
	}
	return f, info, nil
}

// Save streams r into relDir under a sanitised version of name, avoiding
// collisions by appending " (1)", " (2)", … It returns the created entry.
func (b *Browser) Save(relDir, name string, r io.Reader) (Entry, error) {
	dirAbs, err := b.resolve(relDir)
	if err != nil {
		return Entry{}, err
	}
	if err := os.MkdirAll(dirAbs, 0o755); err != nil {
		return Entry{}, err
	}
	safe := safepath.SanitizeFilename(name)
	finalName, dst := b.uniquePath(dirAbs, safe)

	tmp, err := os.CreateTemp(dirAbs, ".myshare-up-*")
	if err != nil {
		return Entry{}, err
	}
	tmpName := tmp.Name()
	buf := make([]byte, 1<<20)
	if _, err := io.CopyBuffer(tmp, r, buf); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return Entry{}, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return Entry{}, err
	}
	tmp.Close()
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return Entry{}, err
	}
	e, _, err := b.Stat(joinRel(b.rel(dirAbs), finalName))
	return e, err
}

// AdoptFile moves an already-complete file at srcPath into relDir under name
// (used by the resumable-upload finaliser). srcPath is consumed.
func (b *Browser) AdoptFile(relDir, name, srcPath string) (Entry, error) {
	dirAbs, err := b.resolve(relDir)
	if err != nil {
		return Entry{}, err
	}
	if err := os.MkdirAll(dirAbs, 0o755); err != nil {
		return Entry{}, err
	}
	finalName, dst := b.uniquePath(dirAbs, safepath.SanitizeFilename(name))
	if err := os.Rename(srcPath, dst); err != nil {
		// Cross-device: stream-copy then remove.
		if cpErr := copyFile(srcPath, dst); cpErr != nil {
			return Entry{}, cpErr
		}
		os.Remove(srcPath)
	}
	e, _, err := b.Stat(joinRel(b.rel(dirAbs), finalName))
	return e, err
}

// Rename changes a file or folder's name within its current directory.
func (b *Browser) Rename(rel, newName string) (Entry, error) {
	abs, err := b.resolve(rel)
	if err != nil {
		return Entry{}, err
	}
	if _, err := os.Stat(abs); err != nil {
		return Entry{}, ErrNotFound
	}
	safe := safepath.SanitizeFilename(newName)
	dir := filepath.Dir(abs)
	finalName, dst := b.uniquePath(dir, safe)
	if err := os.Rename(abs, dst); err != nil {
		return Entry{}, err
	}
	e, _, err := b.Stat(joinRel(b.rel(dir), finalName))
	return e, err
}

// Delete removes a file, or an (empty or non-empty) directory.
func (b *Browser) Delete(rel string) error {
	abs, err := b.resolve(rel)
	if err != nil {
		return err
	}
	if abs == b.root {
		return safepath.ErrUnsafePath
	}
	if _, err := os.Stat(abs); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	return os.RemoveAll(abs)
}

// DeleteContents removes everything inside relDir but keeps relDir itself. The
// hidden meta directory is preserved.
func (b *Browser) DeleteContents(relDir string) (int, error) {
	abs, err := b.resolve(relDir)
	if err != nil {
		return 0, err
	}
	des, err := os.ReadDir(abs)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, de := range des {
		if de.Name() == metaDirName || strings.HasPrefix(de.Name(), ".myshare") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(abs, de.Name())); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Mkdir creates a new subdirectory.
func (b *Browser) Mkdir(relDir, name string) (Entry, error) {
	dirAbs, err := b.resolve(relDir)
	if err != nil {
		return Entry{}, err
	}
	safe := safepath.SanitizeFilename(name)
	target := filepath.Join(dirAbs, safe)
	if !safepath.WithinBase(b.root, target) {
		return Entry{}, safepath.ErrUnsafePath
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		return Entry{}, err
	}
	e, _, err := b.Stat(joinRel(b.rel(dirAbs), safe))
	return e, err
}

// Walk yields every regular file under the root (for "download all as zip").
func (b *Browser) Walk(fn func(relPath string, info os.FileInfo) error) error {
	return filepath.Walk(b.root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		rel := b.rel(p)
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(rel, metaDirName) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		return fn(rel, info)
	})
}

// --- helpers --------------------------------------------------------------

func (b *Browser) uniquePath(dirAbs, name string) (finalName, absPath string) {
	candidate := name
	for i := 1; ; i++ {
		p := filepath.Join(dirAbs, candidate)
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			return candidate, p
		}
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		candidate = fmt.Sprintf("%s (%d)%s", stem, i, ext)
	}
}

func parentOf(rel string) string {
	if rel == "" {
		return ""
	}
	p := path.Dir(rel)
	if p == "." || p == "/" {
		return ""
	}
	return p
}

func joinRel(dir, name string) string {
	if dir == "" || dir == "." {
		return name
	}
	return dir + "/" + name
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), ".myshare-cp-*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	buf := make([]byte, 1<<20)
	if _, err := io.CopyBuffer(out, in, buf); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Sync()
	out.Close()
	return os.Rename(tmp, dst)
}

// FingerprintDir returns a cheap signature of a directory's immediate contents
// (names + sizes + mtimes) so a poller can tell when something changed without
// diffing full trees.
func (b *Browser) FingerprintDir(relDir string) (string, error) {
	l, err := b.List(relDir)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, e := range l.Entries {
		fmt.Fprintf(&sb, "%s\x00%d\x00%d\x00%v\n", e.Name, e.Size, e.ModTime, e.IsDir)
	}
	return sb.String(), nil
}

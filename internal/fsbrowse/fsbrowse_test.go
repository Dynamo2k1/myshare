package fsbrowse

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newTree(t *testing.T) (*Browser, string) {
	t.Helper()
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644))
	must(os.WriteFile(filepath.Join(root, "b.log"), []byte("bravo"), 0o644))
	must(os.MkdirAll(filepath.Join(root, "sub", "deep"), 0o755))
	must(os.WriteFile(filepath.Join(root, "sub", "c.md"), []byte("# charlie"), 0o644))
	must(os.WriteFile(filepath.Join(root, "sub", "deep", "d.bin"), []byte("delta"), 0o644))
	// a secret sibling the browser must never expose
	outside := filepath.Join(filepath.Dir(root), "SECRET.txt")
	must(os.WriteFile(outside, []byte("top secret"), 0o644))
	t.Cleanup(func() { os.Remove(outside) })

	b, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	return b, root
}

func TestListRootAndSub(t *testing.T) {
	b, _ := newTree(t)

	l, err := b.List("")
	if err != nil {
		t.Fatal(err)
	}
	if l.Dir != "" || l.Parent != "" {
		t.Errorf("root listing dir=%q parent=%q", l.Dir, l.Parent)
	}
	names := entryNames(l)
	// folders first, then files, both alpha
	want := []string{"sub", "a.txt", "b.log"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("root entries = %v, want %v", names, want)
	}

	sl, err := b.List("sub")
	if err != nil {
		t.Fatal(err)
	}
	if sl.Dir != "sub" || sl.Parent != "" {
		t.Errorf("sub listing dir=%q parent=%q", sl.Dir, sl.Parent)
	}
	if got := entryNames(sl); strings.Join(got, ",") != "deep,c.md" {
		t.Errorf("sub entries = %v", got)
	}

	dl, _ := b.List("sub/deep")
	if dl.Parent != "sub" {
		t.Errorf("deep parent = %q, want sub", dl.Parent)
	}
}

func TestTraversalRefused(t *testing.T) {
	b, _ := newTree(t)
	hostile := []string{
		"../SECRET.txt",
		"../../etc/passwd",
		"sub/../../SECRET.txt",
		"sub/../../../etc/hosts",
		"/etc/passwd",
		"..",
		"sub/./../../SECRET.txt",
		".myshare",
		".myshare/myshare.db",
	}
	for _, p := range hostile {
		if _, _, err := b.Stat(p); err == nil {
			t.Errorf("Stat(%q) should have failed", p)
		}
		if _, _, err := b.Open(p); err == nil {
			t.Errorf("Open(%q) should have failed", p)
		}
		if _, err := b.List(p); err == nil {
			t.Errorf("List(%q) should have failed", p)
		}
		if err := b.Delete(p); err == nil {
			t.Errorf("Delete(%q) should have failed", p)
		}
	}
}

func TestSymlinkEscapeRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	b, root := newTree(t)
	outside := filepath.Join(filepath.Dir(root), "SECRET.txt")
	link := filepath.Join(root, "peek")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	// The symlink must not appear in listings...
	l, _ := b.List("")
	for _, e := range l.Entries {
		if e.Name == "peek" {
			t.Fatal("escaping symlink appeared in listing")
		}
	}
	// ...and must not be openable.
	if _, _, err := b.Open("peek"); err == nil {
		t.Fatal("opened a symlink that escapes the root")
	}
}

func TestSaveUniqueNames(t *testing.T) {
	b, _ := newTree(t)
	e1, err := b.Save("", "a.txt", strings.NewReader("new alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if e1.Name != "a (1).txt" {
		t.Errorf("first collision name = %q, want 'a (1).txt'", e1.Name)
	}
	e2, _ := b.Save("", "a.txt", strings.NewReader("newer"))
	if e2.Name != "a (2).txt" {
		t.Errorf("second collision name = %q", e2.Name)
	}
	// The original is untouched.
	f, _, _ := b.Open("a.txt")
	buf := make([]byte, 16)
	n, _ := f.Read(buf)
	f.Close()
	if string(buf[:n]) != "alpha" {
		t.Errorf("original a.txt was modified: %q", buf[:n])
	}
}

func TestDeleteRealFileAndContents(t *testing.T) {
	b, root := newTree(t)
	if err := b.Delete("a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Error("a.txt still on disk after Delete")
	}

	n, err := b.DeleteContents("")
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("DeleteContents removed nothing")
	}
	// Root itself survives; it's just empty (bar the meta dir if present).
	if _, err := os.Stat(root); err != nil {
		t.Errorf("root removed by DeleteContents: %v", err)
	}
	l, _ := b.List("")
	if len(l.Entries) != 0 {
		t.Errorf("entries remain after DeleteContents: %v", entryNames(l))
	}
}

func TestMkdirAndRename(t *testing.T) {
	b, _ := newTree(t)
	if _, err := b.Mkdir("", "New Folder"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Stat("New Folder"); err != nil {
		t.Fatalf("mkdir'd folder not found: %v", err)
	}
	e, err := b.Rename("b.log", "renamed.log")
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "renamed.log" {
		t.Errorf("rename result = %q", e.Name)
	}
	if _, _, err := b.Stat("b.log"); err == nil {
		t.Error("old name still resolves after rename")
	}
}

func TestMetaDirHiddenAndCreated(t *testing.T) {
	b, root := newTree(t)
	md, err := b.MetaDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(md) != root || filepath.Base(md) != ".myshare" {
		t.Errorf("meta dir = %q", md)
	}
	// Put a file in it; it must not show in listings.
	os.WriteFile(filepath.Join(md, "myshare.db"), []byte("x"), 0o644)
	l, _ := b.List("")
	for _, e := range l.Entries {
		if strings.HasPrefix(e.Name, ".myshare") {
			t.Fatal(".myshare leaked into a listing")
		}
	}
}

func TestWalkSkipsMeta(t *testing.T) {
	b, _ := newTree(t)
	md, _ := b.MetaDir()
	os.WriteFile(filepath.Join(md, "secret.db"), []byte("x"), 0o644)

	var seen []string
	err := b.Walk(func(rel string, _ os.FileInfo) error {
		seen = append(seen, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range seen {
		if strings.Contains(s, ".myshare") {
			t.Fatalf("Walk yielded a meta-dir file: %s", s)
		}
	}
	// Expect the 4 real files.
	if len(seen) != 4 {
		t.Errorf("Walk saw %d files: %v", len(seen), seen)
	}
}

func entryNames(l Listing) []string {
	out := make([]string, len(l.Entries))
	for i, e := range l.Entries {
		out[i] = e.Name
	}
	return out
}

package safepath

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSanitizeFilename_Traversal(t *testing.T) {
	// Every one of these must come back with no separators and not be a
	// dot-name. The goal isn't a specific output, it's "cannot be a path".
	hostile := []string{
		"../../etc/passwd",
		"..\\..\\windows\\system32\\cmd.exe",
		"/etc/shadow",
		"C:\\Windows\\System32\\evil.dll",
		`\\?\C:\evil`,
		`\\server\share\file`,
		"....//....//etc/passwd",
		"foo/../../bar",
		"..",
		".",
		"...",
		"~/.bashrc",
		"~root/.ssh/authorized_keys",
		"file\x00.txt",
		"a/b/c/d.txt",
		"con",
		"CON",
		"con.txt",
		"NUL.log",
		"com1",
		"LPT9.dat",
		"aux.tar.gz",
		"report.pdf:evil.exe", // NTFS ADS
		"normal.txt::$DATA",   // NTFS ADS
		"  spaced  ",
		"trailingdot...",
		".leadingdot",
		"\u202Egnp.exe",     // RTL override disguising extension
		"invoice\u200B.exe", // zero-width space
		strings.Repeat("A", 5000),
		strings.Repeat("A", 5000) + ".txt",
		"",
		"     ",
		"/",
		"//",
	}

	for _, in := range hostile {
		got := SanitizeFilename(in)
		if got == "" {
			t.Errorf("SanitizeFilename(%q) returned empty", in)
		}
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("SanitizeFilename(%q) = %q contains a separator", in, got)
		}
		if got == "." || got == ".." || got == "..." {
			t.Errorf("SanitizeFilename(%q) = %q is a dot-name", in, got)
		}
		if strings.HasPrefix(got, " ") || strings.HasSuffix(got, " ") {
			t.Errorf("SanitizeFilename(%q) = %q has edge spaces", in, got)
		}
		if strings.HasSuffix(got, ".") {
			t.Errorf("SanitizeFilename(%q) = %q ends with a dot", in, got)
		}
		if strings.ContainsRune(got, 0) {
			t.Errorf("SanitizeFilename(%q) = %q contains NUL", in, got)
		}
		if len(got) > maxNameBytes {
			t.Errorf("SanitizeFilename(%q) = %d bytes, over limit", in, len(got))
		}
		if _, bad := windowsReserved[strings.ToLower(strings.SplitN(got, ".", 2)[0])]; bad {
			t.Errorf("SanitizeFilename(%q) = %q is a reserved device name", in, got)
		}
	}
}

func TestSanitizeFilename_KeepsReasonableNames(t *testing.T) {
	cases := map[string]string{
		"report.pdf":             "report.pdf",
		"My Photo 2026.jpeg":     "My Photo 2026.jpeg",
		"snapshot-v1.2.3.tar.gz": "snapshot-v1.2.3.tar.gz",
		"résumé.docx":            "résumé.docx",
		"data (1).csv":           "data (1).csv",
	}
	for in, want := range cases {
		if got := SanitizeFilename(in); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeFilename_LongNameKeepsExtension(t *testing.T) {
	in := strings.Repeat("x", 400) + ".pdf"
	got := SanitizeFilename(in)
	if len(got) > maxNameBytes {
		t.Fatalf("length %d over limit", len(got))
	}
	if !strings.HasSuffix(got, ".pdf") {
		t.Errorf("extension lost: %q", got)
	}
}

func TestSanitizeFilename_ValidUTF8AfterClamp(t *testing.T) {
	in := strings.Repeat("é", 400) + ".txt" // 2 bytes per rune
	got := SanitizeFilename(in)
	if len(got) > maxNameBytes {
		t.Fatalf("length %d over limit", len(got))
	}
	if !utf8Valid(got) {
		t.Errorf("clamp produced invalid UTF-8: %q", got)
	}
}

func TestResolveInside(t *testing.T) {
	base := filepath.Clean(mustAbs(t, "/srv/myshare/data"))

	ok := []string{"blobs/aa/bb/cc", "myshare.db", "a/b/c.txt", "./x"}
	for _, rel := range ok {
		got, err := ResolveInside(base, rel)
		if err != nil {
			t.Errorf("ResolveInside(%q) unexpected error: %v", rel, err)
			continue
		}
		if !WithinBase(base, got) {
			t.Errorf("ResolveInside(%q) = %q escaped base", rel, got)
		}
	}

	bad := []string{
		"../secret",
		"../../etc/passwd",
		"blobs/../../../../etc/passwd",
		"/etc/passwd",
		"",
		"..",
	}
	for _, rel := range bad {
		if got, err := ResolveInside(base, rel); err == nil {
			t.Errorf("ResolveInside(%q) = %q, expected ErrUnsafePath", rel, got)
		}
	}
}

func TestResolveInside_WindowsShapes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific path shapes")
	}
	base := `C:\srv\myshare\data`
	bad := []string{`C:\Windows`, `\\?\C:\x`, `\\srv\share`, `..\x`, `C:x`}
	for _, rel := range bad {
		if _, err := ResolveInside(base, rel); err == nil {
			t.Errorf("ResolveInside(%q) should have failed", rel)
		}
	}
}

func TestWithinBase(t *testing.T) {
	base := filepath.Clean("/a/b")
	if !WithinBase(base, "/a/b") || !WithinBase(base, "/a/b/c") {
		t.Error("descendant/self should be within base")
	}
	if WithinBase(base, "/a/bc") || WithinBase(base, "/a") || WithinBase(base, "/x") {
		t.Error("sibling/parent/other must not be within base")
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' {
			return false
		}
	}
	return true
}

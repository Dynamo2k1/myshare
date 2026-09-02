// Package safepath contains the filename and path defences for MyShare.
//
// MyShare stores file *bytes* in content-addressed blobs (named by SHA-256), so
// a user-supplied filename never becomes a filesystem path. These helpers still
// exist for defence in depth: to sanitise the display name we persist as
// metadata, and to guarantee that any path we build from an internal component
// stays inside the configured data directory.
package safepath

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ErrUnsafePath is returned when a resolved path escapes its base directory.
var ErrUnsafePath = errors.New("path escapes base directory")

// maxNameBytes bounds a sanitised filename. 255 bytes is the common per-component
// limit on ext4, APFS and NTFS.
const maxNameBytes = 255

// invisibleRunes are zero-width and bidirectional-control code points that can
// disguise a filename's real extension (e.g. "invoice‮gnp.exe"). They are
// stripped entirely during sanitisation.
var invisibleRunes = map[rune]struct{}{
	0x200B: {}, // ZERO WIDTH SPACE
	0x200C: {}, // ZERO WIDTH NON-JOINER
	0x200D: {}, // ZERO WIDTH JOINER
	0x200E: {}, // LEFT-TO-RIGHT MARK
	0x200F: {}, // RIGHT-TO-LEFT MARK
	0x202A: {}, // LEFT-TO-RIGHT EMBEDDING
	0x202B: {}, // RIGHT-TO-LEFT EMBEDDING
	0x202C: {}, // POP DIRECTIONAL FORMATTING
	0x202D: {}, // LEFT-TO-RIGHT OVERRIDE
	0x202E: {}, // RIGHT-TO-LEFT OVERRIDE
	0x2066: {}, // LEFT-TO-RIGHT ISOLATE
	0x2067: {}, // RIGHT-TO-LEFT ISOLATE
	0x2068: {}, // FIRST STRONG ISOLATE
	0x2069: {}, // POP DIRECTIONAL ISOLATE
	0xFEFF: {}, // ZERO WIDTH NO-BREAK SPACE / BOM
}

// windowsReserved are device names that are invalid as a whole filename on
// Windows, with or without an extension. We reject them on every OS so files
// created on Linux stay portable.
var windowsReserved = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {},
	"com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {},
	"lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

// SanitizeFilename reduces an arbitrary, possibly hostile string to a single
// safe filename component. It never returns an empty string: if nothing usable
// survives, it returns "file".
//
// Guarantees about the result:
//   - contains no path separators (/ or \), no drive/colon, no NUL or control chars
//   - is not ".", "..", or a Windows reserved device name
//   - has no leading/trailing spaces or dots
//   - is NFC-normalised and at most 255 bytes (extension preserved where possible)
func SanitizeFilename(name string) string {
	// Take only the last path component the caller might have embedded.
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}

	name = norm.NFC.String(name)

	// Drop everything Windows forbids in a component, plus control chars and
	// the ADS-enabling colon. Also strip a stray leading '~' to avoid shells
	// or UIs later expanding it.
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == 0:
			continue
		case unicode.IsControl(r):
			continue
		case r == '<' || r == '>' || r == ':' || r == '"' ||
			r == '/' || r == '\\' || r == '|' || r == '?' || r == '*':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()

	// Collapse Unicode bidi / zero-width trickery used to disguise extensions.
	out = strings.Map(func(r rune) rune {
		if _, drop := invisibleRunes[r]; drop {
			return -1
		}
		return r
	}, out)

	out = strings.Trim(out, " .")
	out = strings.TrimSpace(out)

	if out == "" || out == "." || out == ".." {
		return "file"
	}

	// Reserved device name check (case-insensitive, on the stem before the dot).
	stem := out
	if i := strings.IndexByte(out, '.'); i > 0 {
		stem = out[:i]
	}
	if _, bad := windowsReserved[strings.ToLower(stem)]; bad {
		out = "_" + out
	}

	out = clampBytesKeepingExt(out, maxNameBytes)

	// Re-trim in case clamping exposed a trailing dot/space.
	out = strings.Trim(out, " .")
	if out == "" {
		return "file"
	}
	return out
}

// clampBytesKeepingExt shortens s to at most max bytes, preserving the file
// extension when it is short enough to be worth keeping, and never cutting a
// UTF-8 sequence in half.
func clampBytesKeepingExt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	ext := filepath.Ext(s)
	if len(ext) > 16 || len(ext) >= max {
		ext = "" // absurd "extension": treat the whole thing as the stem
	}
	stem := s[:len(s)-len(ext)]
	budget := max - len(ext)

	// Largest prefix of stem that fits the budget on a rune boundary.
	cut := len(stem)
	for cut > budget {
		cut--
		for cut > 0 && (stem[cut]&0xC0) == 0x80 { // step back over continuation bytes
			cut--
		}
	}
	if cut < 0 {
		cut = 0
	}
	return stem[:cut] + ext
}

// ResolveInside joins rel onto base and returns the cleaned absolute path,
// guaranteeing the result is base itself or a descendant of it. base must be an
// absolute, cleaned path. rel is treated as untrusted.
func ResolveInside(base, rel string) (string, error) {
	if !filepath.IsAbs(base) {
		return "", errors.New("safepath: base must be absolute")
	}
	base = filepath.Clean(base)

	// filepath.IsLocal rejects absolute paths, "..", drive-relative and (on
	// Windows) reserved names / UNC — exactly the escapes we care about.
	rel = filepath.FromSlash(rel)
	if rel == "" || !filepath.IsLocal(rel) {
		return "", ErrUnsafePath
	}

	joined := filepath.Join(base, rel)
	joined = filepath.Clean(joined)

	if joined != base && !strings.HasPrefix(joined, base+string(filepath.Separator)) {
		return "", ErrUnsafePath
	}
	return joined, nil
}

// WithinBase reports whether target lies at or below base. Both are cleaned; no
// filesystem access is performed.
func WithinBase(base, target string) bool {
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	if base == target {
		return true
	}
	return strings.HasPrefix(target, base+string(filepath.Separator))
}

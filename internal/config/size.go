package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSize converts a human-readable size ("5GB", "500MiB", "1024", "unlimited")
// into a byte count. An empty string or "unlimited"/"0" yields 0 (= no limit).
//
// Both decimal (KB/MB/GB/TB, 1000-based) and binary (KiB/MiB/GiB/TiB, 1024-based)
// units are accepted, case-insensitively. A bare number is treated as bytes.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if strings.EqualFold(s, "unlimited") || strings.EqualFold(s, "none") {
		return 0, nil
	}

	lower := strings.ToLower(s)
	var mult int64 = 1
	var numPart string

	switch {
	case strings.HasSuffix(lower, "kib"):
		mult, numPart = 1<<10, s[:len(s)-3]
	case strings.HasSuffix(lower, "mib"):
		mult, numPart = 1<<20, s[:len(s)-3]
	case strings.HasSuffix(lower, "gib"):
		mult, numPart = 1<<30, s[:len(s)-3]
	case strings.HasSuffix(lower, "tib"):
		mult, numPart = 1<<40, s[:len(s)-3]
	case strings.HasSuffix(lower, "kb"):
		mult, numPart = 1_000, s[:len(s)-2]
	case strings.HasSuffix(lower, "mb"):
		mult, numPart = 1_000_000, s[:len(s)-2]
	case strings.HasSuffix(lower, "gb"):
		mult, numPart = 1_000_000_000, s[:len(s)-2]
	case strings.HasSuffix(lower, "tb"):
		mult, numPart = 1_000_000_000_000, s[:len(s)-2]
	case strings.HasSuffix(lower, "b"):
		mult, numPart = 1, s[:len(s)-1]
	default:
		numPart = s
	}

	numPart = strings.TrimSpace(numPart)
	if numPart == "" {
		return 0, fmt.Errorf("missing number in size %q", s)
	}

	if f, err := strconv.ParseFloat(numPart, 64); err == nil {
		if f < 0 {
			return 0, fmt.Errorf("negative size %q", s)
		}
		return int64(f * float64(mult)), nil
	}
	return 0, fmt.Errorf("invalid size %q", s)
}

// FormatSize renders a byte count as a compact binary-unit string.
func FormatSize(n int64) string {
	if n == 0 {
		return "0 B"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

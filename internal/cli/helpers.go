package cli

import "path/filepath"

// dbPath returns the database file path for a data directory.
func dbPath(dataDir string) string {
	return filepath.Join(dataDir, "myshare.db")
}

// Package diskusage reports free/total space for the filesystem backing a path,
// and detects filesystems where SQLite WAL mode is unsafe.
package diskusage

// Usage describes the filesystem hosting a directory.
type Usage struct {
	Total     uint64 `json:"total"`
	Free      uint64 `json:"free"` // available to an unprivileged process
	Used      uint64 `json:"used"`
	FSType    string `json:"fs_type"`
	UnsafeWAL bool   `json:"unsafe_wal"` // fuse/network FS: use rollback journal, not WAL
}

// Of returns the Usage for the filesystem containing path.
func Of(path string) (Usage, error) {
	return platformUsage(path)
}

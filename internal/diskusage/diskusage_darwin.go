//go:build darwin

package diskusage

import (
	"strings"
	"syscall"
)

func platformUsage(path string) (Usage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Usage{}, err
	}
	bs := uint64(st.Bsize)
	total := st.Blocks * bs
	free := st.Bavail * bs

	fsType := bytesToString(st.Fstypename[:])
	u := Usage{
		Total:  total,
		Free:   free,
		Used:   total - st.Bfree*bs,
		FSType: fsType,
	}
	switch {
	case strings.HasPrefix(fsType, "smb"),
		strings.HasPrefix(fsType, "nfs"),
		strings.HasPrefix(fsType, "webdav"),
		strings.Contains(fsType, "fuse"),
		fsType == "msdos", fsType == "exfat", fsType == "ntfs":
		u.UnsafeWAL = true
	}
	return u, nil
}

func bytesToString(b []int8) string {
	buf := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		buf = append(buf, byte(c))
	}
	return string(buf)
}

//go:build linux

package diskusage

import "syscall"

// Magic numbers for filesystems where SQLite WAL (which relies on shared mmap
// and POSIX byte-range locks) is unreliable or unsupported.
const (
	fuseSuperMagic  = 0x65735546
	nfsSuperMagic   = 0x6969
	cifsMagic       = 0xFF534D42
	smb2Magic       = 0xFE534D42
	ntfsSbMagic     = 0x5346544E
	msdosSuperMagic = 0x4D44
	exfatMagic      = 0x2011BAB0
)

func platformUsage(path string) (Usage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Usage{}, err
	}
	bs := uint64(st.Bsize)
	total := st.Blocks * bs
	free := st.Bavail * bs

	u := Usage{
		Total:  total,
		Free:   free,
		Used:   total - st.Bfree*bs,
		FSType: fsTypeName(int64(st.Type)),
	}
	switch int64(st.Type) {
	case fuseSuperMagic, nfsSuperMagic, cifsMagic, smb2Magic,
		ntfsSbMagic, msdosSuperMagic, exfatMagic:
		u.UnsafeWAL = true
	}
	return u, nil
}

func fsTypeName(magic int64) string {
	switch magic {
	case fuseSuperMagic:
		return "fuse"
	case nfsSuperMagic:
		return "nfs"
	case cifsMagic, smb2Magic:
		return "smb"
	case ntfsSbMagic:
		return "ntfs"
	case msdosSuperMagic:
		return "vfat"
	case exfatMagic:
		return "exfat"
	case 0xEF53:
		return "ext"
	case 0x9123683E:
		return "btrfs"
	case 0x58465342:
		return "xfs"
	case 0x2FC12FC1:
		return "zfs"
	case 0x01021994:
		return "tmpfs"
	default:
		return "unknown"
	}
}

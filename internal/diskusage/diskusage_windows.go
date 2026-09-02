//go:build windows

package diskusage

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func platformUsage(path string) (Usage, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Usage{}, err
	}
	p, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return Usage{}, err
	}

	var freeToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &total, &totalFree); err != nil {
		return Usage{}, err
	}

	u := Usage{
		Total:  total,
		Free:   freeToCaller,
		Used:   total - totalFree,
		FSType: volumeFSType(abs),
	}
	// A UNC path (\\server\share) is a network filesystem: avoid WAL.
	if strings.HasPrefix(abs, `\\`) || strings.EqualFold(u.FSType, "NTFS") == false {
		// Local NTFS is fine for WAL; anything else on Windows (FAT/exFAT/UNC),
		// be conservative.
		if strings.HasPrefix(abs, `\\`) || u.FSType == "FAT32" || u.FSType == "exFAT" || u.FSType == "FAT" {
			u.UnsafeWAL = true
		}
	}
	return u, nil
}

func volumeFSType(path string) string {
	root := filepath.VolumeName(path) + `\`
	rp, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "unknown"
	}
	var nameBuf [windows.MAX_PATH + 1]uint16
	var fsBuf [windows.MAX_PATH + 1]uint16
	var serial, maxCompLen, flags uint32
	err = windows.GetVolumeInformation(rp,
		&nameBuf[0], uint32(len(nameBuf)),
		&serial, &maxCompLen, &flags,
		&fsBuf[0], uint32(len(fsBuf)))
	if err != nil {
		return "unknown"
	}
	return windows.UTF16ToString(fsBuf[:])
}

//go:build !linux && !darwin && !windows

package diskusage

import "errors"

func platformUsage(string) (Usage, error) {
	return Usage{FSType: "unknown"}, errors.New("diskusage: unsupported platform")
}

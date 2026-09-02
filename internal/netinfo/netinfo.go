// Package netinfo discovers the machine's LAN address so MyShare can print a
// URL other devices can reach and encode it in a QR code.
package netinfo

import (
	"net"
	"sort"
	"strings"
)

// virtualIfacePrefixes are interface-name prefixes for bridges and virtual
// adapters that are almost never how another real device reaches this host.
// They are skipped when reporting LAN addresses.
var virtualIfacePrefixes = []string{
	"docker", "br-", "veth", "virbr", "vmnet", "vboxnet",
	"tailscale", "zt", "tun", "tap", "wg", "utun",
}

func isVirtualIface(name string) bool {
	n := strings.ToLower(name)
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// LANIPs returns the machine's non-loopback IPv4 addresses, most-likely-primary
// first (private ranges preferred, then by interface index). Bridges and
// virtual adapters (docker0, br-*, veth*, VPN tunnels, …) are excluded. Empty if
// the host has no usable address.
func LANIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	type cand struct {
		ip      net.IP
		idx     int
		private bool
	}
	var cands []cand
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtualIface(ifi.Name) {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			ip4 := ip.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			cands = append(cands, cand{ip: ip4, idx: ifi.Index, private: ip4.IsPrivate()})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].private != cands[j].private {
			return cands[i].private // private first
		}
		return cands[i].idx < cands[j].idx
	})
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.ip.String())
	}
	return out
}

// PrimaryLANIP returns the single best-guess LAN IP, or "" if none.
func PrimaryLANIP() string {
	if ips := LANIPs(); len(ips) > 0 {
		return ips[0]
	}
	return ""
}

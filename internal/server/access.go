package server

import (
	"net"
	"net/http"
)

// accessControl restricts which client IPs may reach the server:
//
//	"local"  – loopback only
//	"lan"    – loopback + RFC1918 / link-local / IPv6 ULA
//	"public" – everyone (no-op)
//
// It runs before auth and routing, so a blocked address never touches the app —
// a "lan" instance stays LAN-only even if someone port-forwards to it.
func accessControl(mode string) func(http.Handler) http.Handler {
	allow := func(ip net.IP) bool { return true }
	switch mode {
	case "local":
		allow = func(ip net.IP) bool { return ip != nil && ip.IsLoopback() }
	case "lan":
		allow = isLocalNetwork
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mode == "public" {
				next.ServeHTTP(w, r)
				return
			}
			// Use the real TCP peer, NOT X-Forwarded-For / X-Real-IP — those are
			// attacker-controlled unless a trusted proxy sets them, and MyShare
			// is normally exposed directly. This middleware runs before RealIP.
			if allow(peerIP(r)) {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"This MyShare server only accepts connections from its local network.","code":"access_denied"}`))
		})
	}
}

// peerIP returns the IP of the actual TCP connection, ignoring proxy headers.
func peerIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// isLocalNetwork reports whether ip is loopback, private (RFC1918), link-local,
// unique-local (fc00::/7) or a private-use IPv6 address.
func isLocalNetwork(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	// net.IP.IsPrivate already covers fc00::/7 and RFC1918; the link-local check
	// covers 169.254/16 and fe80::/10. Nothing else counts as "LAN".
	return false
}

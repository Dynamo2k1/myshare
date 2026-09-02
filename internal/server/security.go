package server

import (
	"net/http"
	"strings"

	"github.com/dynamo2k1/myshare/internal/config"
)

// securityHeaders sets a strict, same-origin-only policy. The SPA is fully
// self-hosted (no CDNs), so the CSP can forbid inline/eval script outright.
func securityHeaders(cfg config.Config) func(http.Handler) http.Handler {
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'", // Vite injects a tiny style tag; no script risk
		"img-src 'self' data: blob:",
		"media-src 'self' blob:",
		"font-src 'self'",
		"connect-src 'self'",
		"worker-src 'self' blob:",
		"object-src 'none'",
		"base-uri 'none'",
		"frame-ancestors 'none'",
		"form-action 'self'",
	}, "; ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
			if cfg.TLS {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// csrfOriginCheck rejects state-changing API requests whose Origin (or, absent
// that, Referer) is a different host. Combined with SameSite=Lax cookies this
// blocks cross-site request forgery without per-form tokens. Same-origin and
// tool requests (no Origin/Referer, e.g. curl or the CLI) are allowed.
func csrfOriginCheck(cfg config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				// Fall back to Referer's origin.
				if ref := r.Header.Get("Referer"); ref != "" {
					if i := strings.Index(ref, "://"); i >= 0 {
						rest := ref[i+3:]
						if j := strings.IndexByte(rest, '/'); j >= 0 {
							origin = ref[:i+3] + rest[:j]
						} else {
							origin = ref
						}
					}
				}
			}
			if origin == "" {
				// No browser context at all (CLI, curl): allow.
				next.ServeHTTP(w, r)
				return
			}
			if sameHost(origin, r.Host) {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"cross-origin request blocked","code":"csrf"}`))
		})
	}
}

func sameHost(origin, host string) bool {
	oh := origin
	if i := strings.Index(oh, "://"); i >= 0 {
		oh = oh[i+3:]
	}
	if i := strings.IndexByte(oh, '/'); i >= 0 {
		oh = oh[:i]
	}
	// Compare host:port loosely — strip default ports.
	return strings.EqualFold(stripDefaultPort(oh), stripDefaultPort(host))
}

func stripDefaultPort(h string) string {
	h = strings.TrimSuffix(h, ":80")
	h = strings.TrimSuffix(h, ":443")
	return h
}

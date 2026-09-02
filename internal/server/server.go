// Package server assembles the HTTP surface: security headers, the JSON API,
// the tus upload endpoint, SSE, public share links, and the embedded SPA.
package server

import (
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ranauzair/myshare/internal/api"
	"github.com/ranauzair/myshare/internal/auth"
	"github.com/ranauzair/myshare/internal/config"
	"github.com/ranauzair/myshare/internal/sse"
	"github.com/ranauzair/myshare/internal/store"
	"github.com/ranauzair/myshare/web"
)

// Options configures New.
type Options struct {
	Cfg        config.Config
	API        *api.API
	Auth       *auth.Manager
	Hub        *sse.Hub
	DB         *store.DB
	TusHandler http.Handler // mounted under /api/tus/
	Log        *slog.Logger

	// DevProxy, if non-empty, proxies non-API routes to a running Vite dev
	// server (e.g. http://localhost:5173) instead of serving embedded assets.
	DevProxy string
}

// New returns the root http.Handler.
func New(o Options) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(requestLogger(o.Log))
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders(o.Cfg))
	// No global request timeout: large downloads, long uploads and the SSE
	// stream must not be capped. Per-handler deadlines are applied where they
	// make sense (JSON bodies, DB calls via request context).

	// --- auth endpoints (always present; no-ops when auth disabled) ------
	r.Route("/api/auth", func(ar chi.Router) {
		h := &authRoutes{mgr: o.Auth, cfg: o.Cfg, log: o.Log}
		ar.Get("/status", h.status)
		ar.Post("/login", h.login)
		ar.Post("/logout", h.logout)
	})

	// --- JSON API (guarded) --------------------------------------------
	r.Group(func(gr chi.Router) {
		gr.Use(csrfOriginCheck(o.Cfg))
		gr.Use(o.Auth.APIMiddleware)
		gr.Mount("/api", o.API.Routes())
		gr.Get("/api/events", o.Hub.ServeHTTP)
		gr.Get("/api/files/{id}/raw", rawDownload(o.API))
		if o.TusHandler != nil {
			gr.Mount("/api/tus", http.StripPrefix("/api/tus", o.TusHandler))
			gr.Handle("/api/tus/*", http.StripPrefix("/api/tus", o.TusHandler))
		}
	})

	// --- public share links (no auth) --------------------------------
	r.Get("/s/{token}", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("meta") != "" {
			o.API.ShareMetaJSON(w, req)
			return
		}
		o.API.ServePublicShare(w, req)
	})
	r.Head("/s/{token}", o.API.ServePublicShare)

	// --- frontend ----------------------------------------------------
	if o.DevProxy != "" {
		r.NotFound(viteProxy(o.DevProxy, o.Log))
	} else {
		r.NotFound(spaHandler(web.DistFS()))
	}
	return r
}

// rawDownload resolves the file id and streams it via the API's shared
// download path (Range-capable, constant memory).
func rawDownload(a *api.API) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := a.DB.GetFile(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		a.DownloadFile(w, r, f, r.URL.Query().Get("dl") != "")
	}
}

// spaHandler serves static assets and falls back to index.html for client-side
// routes. Hashed assets under /assets get long-lived caching; index.html never
// caches.
func spaHandler(dist fs.FS) http.HandlerFunc {
	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w, "frontend not built", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Never cache the shell: a redeploy changes the hashed asset names it
		// points at, and a stale index.html leaves the browser loading a JS
		// bundle that no longer exists. The assets themselves are immutable.
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		w.Write(b)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "index.html" {
			serveIndex(w, r)
			return
		}
		f, err := dist.Open(name)
		if err != nil {
			// Unknown path with no file extension -> SPA client route.
			serveIndex(w, r)
			return
		}
		st, _ := f.Stat()
		if st != nil && st.IsDir() {
			f.Close()
			serveIndex(w, r)
			return
		}
		defer f.Close()

		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		if seeker, ok := f.(io.ReadSeeker); ok {
			http.ServeContent(w, r, name, time.Time{}, seeker)
			return
		}
		_, _ = io.Copy(w, f)
	}
}

func viteProxy(target string, log *slog.Logger) http.HandlerFunc {
	u, err := url.Parse(target)
	if err != nil {
		log.Error("bad --dev-proxy url", "target", target, "err", err)
		return func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad dev proxy", http.StatusInternalServerError)
		}
	}
	p := httputil.NewSingleHostReverseProxy(u)
	return p.ServeHTTP
}

// quietPollPaths are polled every few seconds by every open tab; they log at
// debug so the info stream stays readable. Everything else logs at info.
var quietPollPaths = map[string]bool{
	"/api/status":    true,
	"/api/transfers": true,
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			if r.URL.Path == "/api/events" {
				return // the SSE stream stays open; logging it is meaningless
			}

			dur := time.Since(start).Round(time.Millisecond)
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"dur", dur.String(),
				"ip", clientIP(r),
			}
			switch {
			case ww.Status() >= 500:
				log.Error("request", attrs...)
			case ww.Status() >= 400:
				log.Warn("request", attrs...)
			case r.Method == http.MethodGet && quietPollPaths[r.URL.Path]:
				log.Debug("request", attrs...)
			default:
				log.Info("request", attrs...)
			}
		})
	}
}

func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		if i := strings.IndexByte(h, ','); i > 0 {
			return strings.TrimSpace(h[:i])
		}
		return strings.TrimSpace(h)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

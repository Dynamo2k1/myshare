package api

import (
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5"

	"github.com/ranauzair/myshare/internal/diskusage"
	"github.com/ranauzair/myshare/internal/store"
)

// Version is stamped by the linker at build time; falls back to the module
// version embedded by `go build`.
var Version = "dev"

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	hits, err := a.DB.Search(r.Context(), r.URL.Query().Get("q"), queryInt(r, "limit", 30))
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	if hits == nil {
		hits = []store.SearchHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": hits})
}

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	st, err := a.DB.Stats(r.Context())
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	du, _ := diskusage.Of(a.Cfg.DataDir)

	resp := map[string]any{
		"version":       version(),
		"host":          a.Cfg.Host,
		"port":          a.Cfg.Port,
		"data_dir":      a.Cfg.DataDir,
		"tls":           a.Cfg.TLS,
		"auth":          a.Cfg.Auth,
		"stats":         st,
		"connected":     a.Hub.Count(),
		"disk":          du,
		"max_file_size": a.Cfg.MaxFileSize,
		"max_storage":   a.Cfg.MaxStorage,
	}
	if a.Cfg.MaxStorage > 0 {
		resp["storage_used_pct"] = float64(st.BlobBytes) / float64(a.Cfg.MaxStorage) * 100
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) listTransfers(w http.ResponseWriter, r *http.Request) {
	list, err := a.DB.ListUploadSessions(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (a *API) removeTransfer(w http.ResponseWriter, r *http.Request) {
	if err := a.DB.DeleteUploadSession(r.Context(), chi.URLParam(r, "id")); err != nil {
		a.failStore(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func version() string {
	if Version != "dev" && Version != "" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// Package api implements MyShare's REST endpoints. Everything is addressed by
// opaque ID; no handler ever accepts or returns a filesystem path.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/dynamo2k1/myshare/internal/blob"
	"github.com/dynamo2k1/myshare/internal/config"
	"github.com/dynamo2k1/myshare/internal/fsbrowse"
	"github.com/dynamo2k1/myshare/internal/sse"
	"github.com/dynamo2k1/myshare/internal/store"
)

// API bundles the dependencies every handler needs.
type API struct {
	DB   *store.DB
	Blob *blob.Store
	Hub  *sse.Hub
	Cfg  config.Config
	Log  *slog.Logger

	// TempDir is where direct (non-tus) uploads are streamed before adoption.
	TempDir string

	// Browser is set only in directory mode; when non-nil the Files tab browses
	// a real folder instead of the content-addressed blob store.
	Browser *fsbrowse.Browser
}

// Routes returns the chi router for everything under /api (auth middleware is
// applied by the caller).
func (a *API) Routes() chi.Router {
	r := chi.NewRouter()

	if a.Browser != nil {
		// Directory mode: the Files tab browses a real folder.
		r.Get("/browse", a.browseList)
		r.Post("/browse", a.browseUpload)
		r.Patch("/browse", a.browsePatch)
		r.Delete("/browse", a.browseDelete)
		r.Get("/browse/raw", a.browseRaw)
		r.Get("/browse/archive.zip", a.browseArchive)
		r.Delete("/browse/all", a.browseDeleteAll)
		r.Post("/browse/mkdir", a.browseMkdir)
	} else {
		r.Get("/files", a.listFiles)
		r.Post("/files", a.uploadFileDirect)
		r.Delete("/files", a.deleteAllFiles)
		r.Get("/files/archive.zip", a.downloadArchive)
		r.Get("/files/{id}", a.getFile)
		r.Patch("/files/{id}", a.patchFile)
		r.Delete("/files/{id}", a.deleteFile)
	}

	r.Get("/clipboard", a.listClipboard)
	r.Post("/clipboard", a.createClipboard)
	r.Delete("/clipboard", a.clearClipboard)
	r.Patch("/clipboard/{id}", a.patchClipboard)
	r.Delete("/clipboard/{id}", a.deleteClipboard)

	r.Get("/snippets", a.listSnippets)
	r.Post("/snippets", a.createSnippet)
	r.Get("/snippets/{id}", a.getSnippet)
	r.Patch("/snippets/{id}", a.patchSnippet)
	r.Delete("/snippets/{id}", a.deleteSnippet)
	r.Post("/snippets/{id}/duplicate", a.duplicateSnippet)

	r.Get("/notes", a.listNotes)
	r.Post("/notes", a.createNote)
	r.Get("/notes/{id}", a.getNote)
	r.Patch("/notes/{id}", a.patchNote)
	r.Delete("/notes/{id}", a.deleteNote)

	r.Get("/shares", a.listSharesQuery)
	r.Post("/shares", a.createShare)
	r.Delete("/shares/{id}", a.revokeShare)

	r.Get("/transfers", a.listTransfers)
	r.Delete("/transfers/{id}", a.removeTransfer)

	r.Get("/scratch", a.getScratch)
	r.Put("/scratch", a.putScratch)

	r.Get("/search", a.search)
	r.Get("/status", a.status)

	return r
}

// --- response helpers ---------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// fail writes a human-readable error. Internal detail is logged, never sent.
func (a *API) fail(w http.ResponseWriter, r *http.Request, status int, code, msg string, cause error) {
	if cause != nil && status >= 500 {
		a.Log.Error("request failed",
			"method", r.Method, "path", r.URL.Path, "status", status, "err", cause)
	}
	writeJSON(w, status, apiError{Error: msg, Code: code})
}

// failStore maps common store errors to HTTP responses.
func (a *API) failStore(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.fail(w, r, http.StatusNotFound, "not_found", "That item does not exist.", nil)
	case errors.Is(err, store.ErrShareUnavailable):
		a.fail(w, r, http.StatusGone, "share_unavailable", "This link is no longer available.", nil)
	default:
		a.fail(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Please try again.", err)
	}
}

// decodeJSON reads a JSON body with a sane size cap.
func decodeJSON(r *http.Request, dst any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func originID(r *http.Request) string {
	return r.Header.Get("X-MyShare-Client")
}

func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func queryBool(r *http.Request, key string) bool {
	v := strings.ToLower(r.URL.Query().Get(key))
	return v == "1" || v == "true" || v == "yes"
}

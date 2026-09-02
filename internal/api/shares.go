package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ranauzair/myshare/internal/shares"
	"github.com/ranauzair/myshare/internal/store"
)

type createShareReq struct {
	FileID       string `json:"file_id"`
	ExpiresInSec *int64 `json:"expires_in_sec"` // nil = never
	MaxDownloads *int64 `json:"max_downloads"`  // nil = unlimited
	OneTime      bool   `json:"one_time"`
}

type shareView struct {
	store.Share
	URL string `json:"url"`
}

func (a *API) createShare(w http.ResponseWriter, r *http.Request) {
	var req createShareReq
	if err := decodeJSON(r, &req, 1<<16); err != nil {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "Invalid request body.", nil)
		return
	}
	if req.FileID == "" {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "file_id is required.", nil)
		return
	}
	if _, err := a.DB.GetFile(r.Context(), req.FileID); err != nil {
		a.failStore(w, r, err)
		return
	}

	token, hash, err := shares.NewToken()
	if err != nil {
		a.fail(w, r, http.StatusInternalServerError, "internal", "Could not create the link.", err)
		return
	}
	opt := store.ShareCreateOptions{FileID: req.FileID, TokenHash: hash, OneTime: req.OneTime}
	if req.ExpiresInSec != nil && *req.ExpiresInSec > 0 {
		exp := time.Now().Unix() + *req.ExpiresInSec
		opt.ExpiresAt = &exp
	}
	if req.MaxDownloads != nil && *req.MaxDownloads > 0 {
		opt.MaxDownloads = req.MaxDownloads
	}

	sh, err := a.DB.CreateShare(r.Context(), opt)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	// The plaintext token is returned exactly once, here.
	writeJSON(w, http.StatusCreated, shareView{Share: sh, URL: a.shareURL(r, token)})
}

func (a *API) listSharesQuery(w http.ResponseWriter, r *http.Request) {
	fileID := r.URL.Query().Get("file_id")
	if fileID == "" {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "file_id query parameter is required.", nil)
		return
	}
	list, err := a.DB.ListSharesForFile(r.Context(), fileID)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	// Existing shares cannot expose their token again; return metadata only.
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (a *API) revokeShare(w http.ResponseWriter, r *http.Request) {
	if err := a.DB.RevokeShare(r.Context(), chi.URLParam(r, "id")); err != nil {
		a.failStore(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ServePublicShare backs GET /s/{token}: it resolves the token, streams the
// file, then records the download (which may auto-revoke a one-time link).
func (a *API) ServePublicShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if !shares.ValidTokenShape(token) {
		a.fail(w, r, http.StatusNotFound, "not_found", "This link is not valid.", nil)
		return
	}
	sh, f, err := a.DB.ResolveShareToken(r.Context(), shares.HashToken(token))
	if err != nil {
		a.failStore(w, r, err)
		return
	}

	// Public downloads are always attachments regardless of type.
	a.DownloadFile(w, r, f, true)

	// Only count a full 200; a Range/conditional request that returned 206/304
	// is a resumed or cached fetch of the same download.
	if r.Method == http.MethodGet {
		if err := a.DB.RecordShareDownload(r.Context(), sh.ID); err != nil {
			a.Log.Warn("record share download", "share", sh.ID, "err", err)
		}
	}
}

// ShareMetaJSON backs GET /s/{token}?meta=1 for the SPA's preview page.
func (a *API) ShareMetaJSON(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if !shares.ValidTokenShape(token) {
		a.fail(w, r, http.StatusNotFound, "not_found", "This link is not valid.", nil)
		return
	}
	_, f, err := a.DB.ResolveShareToken(r.Context(), shares.HashToken(token))
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": f.Name, "size": f.Size, "mime": f.MIME,
		"download_url": "/s/" + token,
	})
}

func (a *API) shareURL(r *http.Request, token string) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" || a.Cfg.TLS {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = a.Cfg.Host
	}
	return scheme + "://" + host + "/s/" + token
}

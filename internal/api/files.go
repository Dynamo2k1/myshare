package api

import (
	"archive/zip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dynamo2k1/myshare/internal/safepath"
	"github.com/dynamo2k1/myshare/internal/sse"
	"github.com/dynamo2k1/myshare/internal/store"
)

// inlineTypes are the only content types served with an inline disposition.
// Everything else is forced to download, defeating stored-XSS via uploaded
// HTML/SVG on this same-origin host.
var inlineTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true,
	"image/webp": true, "image/avif": true, "image/bmp": true,
	"video/mp4": true, "video/webm": true,
	"audio/mpeg": true, "audio/ogg": true, "audio/wav": true, "audio/mp4": true,
	"application/pdf": true,
	"text/plain":      true,
}

func (a *API) listFiles(w http.ResponseWriter, r *http.Request) {
	opt := store.FileListOptions{
		Kind:   r.URL.Query().Get("kind"),
		Sort:   r.URL.Query().Get("sort"),
		Desc:   r.URL.Query().Get("dir") != "asc",
		Search: r.URL.Query().Get("q"),
		Limit:  queryInt(r, "limit", 50),
		Cursor: r.URL.Query().Get("cursor"),
	}
	if r.URL.Query().Get("dir") == "" && opt.Sort == "" {
		opt.Desc = true // default: newest first
	}
	page, err := a.DB.ListFiles(r.Context(), opt)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *API) getFile(w http.ResponseWriter, r *http.Request) {
	f, err := a.DB.GetFile(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

type patchFileReq struct {
	Name *string `json:"name"`
}

func (a *API) patchFile(w http.ResponseWriter, r *http.Request) {
	var req patchFileReq
	if err := decodeJSON(r, &req, 1<<16); err != nil {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "Invalid request body.", nil)
		return
	}
	if req.Name == nil || strings.TrimSpace(*req.Name) == "" {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "A name is required.", nil)
		return
	}
	name := safepath.SanitizeFilename(*req.Name)
	f, err := a.DB.RenameFile(r.Context(), chi.URLParam(r, "id"), name)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "file.updated", Data: f})
	writeJSON(w, http.StatusOK, f)
}

func (a *API) deleteFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	hash, orphaned, err := a.DB.DeleteFile(r.Context(), id)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	if orphaned {
		if err := a.Blob.Remove(hash); err != nil {
			a.Log.Warn("blob remove after delete", "hash", hash, "err", err)
		}
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "file.deleted", Data: map[string]string{"id": id}})
	w.WriteHeader(http.StatusNoContent)
}

// deleteAllFiles bulk-deletes every live file (optionally ?kind=screenshot),
// releasing blob references and removing orphaned bytes.
func (a *API) deleteAllFiles(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != "file" && kind != "screenshot" {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "Unknown kind.", nil)
		return
	}
	hashes, n, err := a.DB.DeleteAllFiles(r.Context(), kind)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	for _, h := range hashes {
		if err := a.Blob.Remove(h); err != nil {
			a.Log.Warn("blob remove after delete-all", "hash", h, "err", err)
		}
	}
	a.Hub.Broadcast(sse.Event{Type: "file.deleted", Data: map[string]any{"all": true, "kind": kind}})
	a.Log.Info("deleted all files", "count", n, "kind", kind)
	writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
}

// downloadArchive streams a .zip of every live file (optionally ?kind=). The zip
// is written straight to the response with a bounded buffer — no file, and no
// whole archive, is ever held in memory.
func (a *API) downloadArchive(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	files, err := a.DB.AllLiveFiles(r.Context(), kind)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	if len(files) == 0 {
		a.fail(w, r, http.StatusNotFound, "empty", "There are no files to download.", nil)
		return
	}

	name := "myshare-files"
	if kind == "screenshot" {
		name = "myshare-screenshots"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", name+"-"+time.Now().Format("2006-01-02")+".zip"))

	zw := zip.NewWriter(w)
	defer zw.Close()

	buf := make([]byte, 1<<20)
	used := map[string]int{}
	for _, f := range files {
		entryName := uniqueName(used, safepath.SanitizeFilename(f.Name))
		hdr := &zip.FileHeader{
			Name:     entryName,
			Method:   zip.Store, // uploads are usually already compressed
			Modified: time.Unix(f.UpdatedAt, 0),
		}
		hdr.SetMode(0o644)
		ew, err := zw.CreateHeader(hdr)
		if err != nil {
			a.Log.Warn("zip entry", "file", f.ID, "err", err)
			return
		}
		blob, err := a.Blob.Open(f.Hash)
		if err != nil {
			a.Log.Warn("zip open blob", "file", f.ID, "hash", f.Hash, "err", err)
			continue
		}
		_, err = io.CopyBuffer(ew, blob, buf)
		blob.Close()
		if err != nil {
			a.Log.Warn("zip copy", "file", f.ID, "err", err)
			return // client likely disconnected
		}
	}
}

func uniqueName(used map[string]int, name string) string {
	if used[name] == 0 {
		used[name] = 1
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for {
		used[name]++
		cand := stem + "-" + strconv.Itoa(used[name]) + ext
		if used[cand] == 0 {
			used[cand] = 1
			return cand
		}
	}
}

// uploadFileDirect handles a plain multipart or raw-body upload for small files.
// Large files go through the tus endpoint instead. The whole body is streamed to
// disk through the blob store; it is never buffered in memory.
func (a *API) uploadFileDirect(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "screenshot" {
		kind = "file"
	}

	var (
		src      io.Reader
		rawName  string
		declared string // client-declared content type (advisory only)
	)

	ct := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		file, hdr, err := r.FormFile("file")
		if err != nil {
			a.fail(w, r, http.StatusBadRequest, "bad_request", "No file field in upload.", nil)
			return
		}
		defer file.Close()
		src, rawName, declared = file, hdr.Filename, hdr.Header.Get("Content-Type")
	default:
		src = r.Body
		rawName = r.URL.Query().Get("name")
		declared = ct
	}

	if a.Cfg.MaxFileSize > 0 {
		src = io.LimitReader(src, a.Cfg.MaxFileSize+1)
	}
	if err := a.checkStorage(r); err != nil {
		a.fail(w, r, http.StatusInsufficientStorage, "storage_full", err.Error(), nil)
		return
	}

	name := safepath.SanitizeFilename(rawName)
	if name == "file" && kind == "screenshot" {
		name = "screenshot-" + time.Now().Format("2006-01-02-150405") + guessExt(declared)
	}

	hash, n, _, err := a.Blob.WriteFrom(src, a.TempDir)
	if err != nil {
		a.fail(w, r, http.StatusInternalServerError, "internal", "Upload failed while saving.", err)
		return
	}
	if a.Cfg.MaxFileSize > 0 && n > a.Cfg.MaxFileSize {
		_ = a.Blob.Remove(hash)
		a.fail(w, r, http.StatusRequestEntityTooLarge, "too_large",
			fmt.Sprintf("File exceeds the %s limit.", human(a.Cfg.MaxFileSize)), nil)
		return
	}

	mimeType := sniffType(name, declared)

	// Duplicate detection: same bytes already stored under a live file.
	if existing, err := a.DB.FileByHash(r.Context(), hash); err == nil {
		f, err := a.DB.CreateFile(r.Context(), name, kind, mimeType, n, hash)
		if err != nil {
			a.failStore(w, r, err)
			return
		}
		a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "file.created", Data: f})
		writeJSON(w, http.StatusCreated, map[string]any{"file": f, "duplicate_of": existing.ID})
		return
	}

	f, err := a.DB.CreateFile(r.Context(), name, kind, mimeType, n, hash)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "file.created", Data: f})
	writeJSON(w, http.StatusCreated, map[string]any{"file": f})
}

// DownloadFile streams a file's bytes with Range support and constant memory.
// It is registered by the server outside the JSON API group so it can also back
// public share links.
func (a *API) DownloadFile(w http.ResponseWriter, r *http.Request, f store.File, forceAttachment bool) {
	rc, err := a.Blob.Open(f.Hash)
	if err != nil {
		a.fail(w, r, http.StatusNotFound, "not_found", "The file's data is missing.", err)
		return
	}
	defer rc.Close()

	disposition := "attachment"
	if !forceAttachment && inlineTypes[f.MIME] {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", f.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q; filename*=UTF-8''%s",
		disposition, asciiFallback(f.Name), urlEncode(f.Name)))
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("ETag", `"`+f.Hash+`"`)

	// http.ServeContent handles Range, If-Range, If-None-Match and HEAD.
	http.ServeContent(w, r, f.Name, time.Unix(f.UpdatedAt, 0), rc)
}

func (a *API) checkStorage(r *http.Request) error {
	if a.Cfg.MaxStorage <= 0 {
		return nil
	}
	st, err := a.DB.Stats(r.Context())
	if err != nil {
		return nil // don't block uploads on a stats hiccup
	}
	if st.BlobBytes >= a.Cfg.MaxStorage {
		return fmt.Errorf("storage limit of %s reached", human(a.Cfg.MaxStorage))
	}
	return nil
}

// --- helpers ----------------------------------------------------------------

func sniffType(name, declared string) string {
	if ext := strings.ToLower(filepath.Ext(name)); ext != "" {
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}
	declared = strings.TrimSpace(strings.SplitN(declared, ";", 2)[0])
	if declared != "" && declared != "application/octet-stream" && !strings.Contains(declared, "form-data") {
		return declared
	}
	return "application/octet-stream"
}

func guessExt(ct string) string {
	switch strings.SplitN(ct, ";", 2)[0] {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func asciiFallback(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" {
		return "download"
	}
	return s
}

func urlEncode(s string) string {
	const hexd = "0123456789ABCDEF"
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hexd[c>>4])
			b.WriteByte(hexd[c&0xf])
		}
	}
	return b.String()
}

func human(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(u), 0
	for x := n / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

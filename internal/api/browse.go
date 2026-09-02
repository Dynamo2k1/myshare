package api

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dynamo2k1/myshare/internal/fsbrowse"
	"github.com/dynamo2k1/myshare/internal/safepath"
	"github.com/dynamo2k1/myshare/internal/sse"
)

// These handlers back "directory mode": the Files tab browses a real folder on
// disk. They are mounted at /api/browse only when a.Browser is set.

func (a *API) browseErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, fsbrowse.ErrNotFound):
		a.fail(w, r, http.StatusNotFound, "not_found", "That path does not exist.", nil)
	case errors.Is(err, safepath.ErrUnsafePath):
		a.fail(w, r, http.StatusBadRequest, "bad_path", "That path is not allowed.", nil)
	default:
		a.fail(w, r, http.StatusInternalServerError, "internal", "Something went wrong.", err)
	}
}

func (a *API) browseList(w http.ResponseWriter, r *http.Request) {
	l, err := a.Browser.List(r.URL.Query().Get("path"))
	if err != nil {
		a.browseErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (a *API) browseRaw(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	f, info, err := a.Browser.Open(rel)
	if err != nil {
		a.browseErr(w, r, err)
		return
	}
	defer f.Close()

	name := info.Name()
	mimeType := sniffType(name, "")
	disposition := "attachment"
	if r.URL.Query().Get("dl") == "" && inlineTypes[mimeType] {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q; filename*=UTF-8''%s",
		disposition, asciiFallback(name), urlEncode(name)))
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func (a *API) browseUpload(w http.ResponseWriter, r *http.Request) {
	relDir := r.URL.Query().Get("path")

	var (
		src     io.Reader
		rawName string
	)
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		file, hdr, err := r.FormFile("file")
		if err != nil {
			a.fail(w, r, http.StatusBadRequest, "bad_request", "No file field in upload.", nil)
			return
		}
		defer file.Close()
		src, rawName = file, hdr.Filename
	} else {
		src, rawName = r.Body, r.URL.Query().Get("name")
	}
	if a.Cfg.MaxFileSize > 0 {
		src = io.LimitReader(src, a.Cfg.MaxFileSize+1)
	}
	if rawName == "" {
		rawName = "upload-" + time.Now().Format("2006-01-02-150405")
	}

	e, err := a.Browser.Save(relDir, rawName, src)
	if err != nil {
		a.browseErr(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "browse.changed", Data: map[string]string{"dir": e.Path}})
	writeJSON(w, http.StatusCreated, map[string]any{"entry": e})
}

func (a *API) browsePatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req, 1<<16); err != nil || strings.TrimSpace(req.Name) == "" {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "A new name is required.", nil)
		return
	}
	e, err := a.Browser.Rename(r.URL.Query().Get("path"), req.Name)
	if err != nil {
		a.browseErr(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "browse.changed", Data: map[string]string{"dir": e.Path}})
	writeJSON(w, http.StatusOK, map[string]any{"entry": e})
}

func (a *API) browseDelete(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if err := a.Browser.Delete(rel); err != nil {
		a.browseErr(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "browse.changed", Data: map[string]string{"path": rel}})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) browseDeleteAll(w http.ResponseWriter, r *http.Request) {
	n, err := a.Browser.DeleteContents(r.URL.Query().Get("path"))
	if err != nil {
		a.browseErr(w, r, err)
		return
	}
	a.Hub.Broadcast(sse.Event{Type: "browse.changed", Data: map[string]any{"cleared": true}})
	writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
}

func (a *API) browseMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req, 1<<16); err != nil || strings.TrimSpace(req.Name) == "" {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "A folder name is required.", nil)
		return
	}
	e, err := a.Browser.Mkdir(r.URL.Query().Get("path"), req.Name)
	if err != nil {
		a.browseErr(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "browse.changed", Data: map[string]string{"dir": e.Path}})
	writeJSON(w, http.StatusCreated, map[string]any{"entry": e})
}

// browseArchive streams a zip of the entire served tree (folder structure
// preserved). Streamed — never buffers the archive or any file in memory.
func (a *API) browseArchive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", "folder-"+time.Now().Format("2006-01-02")+".zip"))

	zw := zip.NewWriter(w)
	defer zw.Close()
	buf := make([]byte, 1<<20)

	err := a.Browser.Walk(func(rel string, info os.FileInfo) error {
		f, _, oerr := a.Browser.Open(rel)
		if oerr != nil {
			return nil
		}
		defer f.Close()
		hdr := &zip.FileHeader{Name: rel, Method: zip.Store, Modified: info.ModTime()}
		hdr.SetMode(0o644)
		ew, cerr := zw.CreateHeader(hdr)
		if cerr != nil {
			return cerr
		}
		_, cerr = io.CopyBuffer(ew, f, buf)
		return cerr
	})
	if err != nil {
		a.Log.Warn("browse archive", "err", err)
	}
}

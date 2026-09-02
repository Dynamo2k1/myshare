// Package uploads wires the tus resumable-upload protocol into MyShare.
//
// tusd streams each PATCH straight to uploads/<id>.bin — no buffering, no size
// ceiling from memory. When an upload completes, a finalizer goroutine hashes
// the file with streaming I/O, moves it into the content-addressed blob store
// (an atomic rename on the common filesystem), writes the files row, and
// broadcasts the change. Interructions resume via the tus HEAD/offset flow;
// the browser keeps its upload URL in localStorage.
package uploads

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/tus/tusd/v2/pkg/filestore"
	tushandler "github.com/tus/tusd/v2/pkg/handler"
	"github.com/tus/tusd/v2/pkg/memorylocker"
	xslog "golang.org/x/exp/slog"

	"github.com/dynamo2k1/myshare/internal/blob"
	"github.com/dynamo2k1/myshare/internal/fsbrowse"
	"github.com/dynamo2k1/myshare/internal/safepath"
	"github.com/dynamo2k1/myshare/internal/sse"
	"github.com/dynamo2k1/myshare/internal/store"
)

// Deps are what the upload subsystem needs from the rest of the app.
type Deps struct {
	DB          *store.DB
	Blob        *blob.Store
	Hub         *sse.Hub
	Log         *slog.Logger
	UploadDir   string            // where tusd keeps in-progress .bin/.info files
	MaxFileSize int64             // 0 = unlimited
	MaxStorage  int64             // 0 = unlimited
	BasePath    string            // e.g. "/api/tus/"
	Browser     *fsbrowse.Browser // set in directory mode: finalise into the served folder
}

// Manager owns the tus handler and its finalizer loop.
type Manager struct {
	deps    Deps
	handler *tushandler.Handler
	stop    chan struct{}
}

// New constructs the tus handler and starts the finalizer goroutine. Call
// Shutdown to stop it.
func New(d Deps) (*Manager, error) {
	if err := os.MkdirAll(d.UploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	fs := filestore.New(d.UploadDir)
	composer := tushandler.NewStoreComposer()
	fs.UseIn(composer)
	memorylocker.New().UseIn(composer)

	m := &Manager{deps: d, stop: make(chan struct{})}

	cfg := tushandler.Config{
		BasePath:                d.BasePath,
		StoreComposer:           composer,
		MaxSize:                 d.MaxFileSize,
		Logger:                  xslog.New(discardHandler{}),
		NotifyCompleteUploads:   true,
		NotifyCreatedUploads:    true,
		NotifyTerminatedUploads: true,
		NotifyUploadProgress:    true,
		DisableDownload:         true, // downloads go through MyShare's own handler
		RespectForwardedHeaders: false,
		PreUploadCreateCallback: m.preCreate,
	}

	h, err := tushandler.NewHandler(cfg)
	if err != nil {
		return nil, fmt.Errorf("tus handler: %w", err)
	}
	m.handler = h

	go m.run()
	return m, nil
}

// Handler is the http.Handler for the tus endpoint. Mount it at BasePath.
func (m *Manager) Handler() http.Handler { return m.handler }

// Shutdown stops the finalizer loop.
func (m *Manager) Shutdown() { close(m.stop) }

// preCreate enforces size and storage limits before a single byte is written,
// and records a tracking row for the Transfers tab.
func (m *Manager) preCreate(hook tushandler.HookEvent) (tushandler.HTTPResponse, tushandler.FileInfoChanges, error) {
	info := hook.Upload
	if m.deps.MaxFileSize > 0 && info.Size > m.deps.MaxFileSize {
		return tushandler.HTTPResponse{
			StatusCode: http.StatusRequestEntityTooLarge,
			Body:       fmt.Sprintf("File exceeds the %s limit.", human(m.deps.MaxFileSize)),
		}, tushandler.FileInfoChanges{}, errors.New("upload too large")
	}
	if m.deps.MaxStorage > 0 {
		if st, err := m.deps.DB.Stats(hook.Context); err == nil {
			if st.BlobBytes+info.Size > m.deps.MaxStorage {
				return tushandler.HTTPResponse{
					StatusCode: http.StatusInsufficientStorage,
					Body:       "Not enough storage space for this upload.",
				}, tushandler.FileInfoChanges{}, errors.New("storage full")
			}
		}
	}
	return tushandler.HTTPResponse{}, tushandler.FileInfoChanges{}, nil
}

func (m *Manager) run() {
	for {
		select {
		case <-m.stop:
			return
		case ev := <-m.handler.CreatedUploads:
			m.onCreated(ev)
		case ev := <-m.handler.UploadProgress:
			m.onProgress(ev)
		case ev := <-m.handler.TerminatedUploads:
			m.onTerminated(ev)
		case ev := <-m.handler.CompleteUploads:
			m.onComplete(ev)
		}
	}
}

func (m *Manager) onCreated(ev tushandler.HookEvent) {
	name, kind, mimeType := metaOf(ev.Upload.MetaData)
	sess := store.UploadSession{
		ID: ev.Upload.ID, Name: name, Kind: kind, MIME: mimeType,
		Size: ev.Upload.Size, Offset: ev.Upload.Offset, Status: "active",
	}
	if err := m.deps.DB.UpsertUploadSession(context.Background(), sess); err != nil {
		m.deps.Log.Warn("track upload create", "id", ev.Upload.ID, "err", err)
	}
	m.deps.Hub.Broadcast(sse.Event{Type: "transfer.created", Data: sess})
	m.deps.Log.Info("upload started", "id", ev.Upload.ID, "name", name, "size", ev.Upload.Size)
}

func (m *Manager) onProgress(ev tushandler.HookEvent) {
	_ = m.deps.DB.SetUploadProgress(context.Background(), ev.Upload.ID, ev.Upload.Offset)
	m.deps.Hub.Broadcast(sse.Event{Type: "transfer.progress", Data: map[string]any{
		"id": ev.Upload.ID, "offset": ev.Upload.Offset, "size": ev.Upload.Size,
	}})
}

func (m *Manager) onTerminated(ev tushandler.HookEvent) {
	_ = m.deps.DB.FailUploadSession(context.Background(), ev.Upload.ID)
	m.deps.Hub.Broadcast(sse.Event{Type: "transfer.removed", Data: map[string]string{"id": ev.Upload.ID}})
	m.deps.Log.Info("upload terminated", "id", ev.Upload.ID)
}

// onComplete finalises a finished upload. All I/O here is streamed.
func (m *Manager) onComplete(ev tushandler.HookEvent) {
	ctx := context.Background()
	id := ev.Upload.ID
	binPath := m.binPath(ev)
	name, kind, mimeType := metaOf(ev.Upload.MetaData)

	m.deps.Log.Info("upload completed", "id", id, "bytes", ev.Upload.Size)

	// Directory mode: move the finished upload straight into the served folder
	// at the target subdir (tus metadata "dir"), no hashing or blob store.
	if m.deps.Browser != nil {
		targetDir := ev.Upload.MetaData["dir"]
		e, err := m.deps.Browser.AdoptFile(targetDir, name, binPath)
		if err != nil {
			m.finalizeFailed(ctx, id, "adopt into folder", err)
			return
		}
		_ = os.Remove(binPath + ".info")
		if err := m.deps.DB.CompleteUploadSession(ctx, id, e.Path, ev.Upload.Size); err != nil {
			m.deps.Log.Warn("mark upload complete", "id", id, "err", err)
		}
		m.deps.Hub.Broadcast(sse.Event{Type: "transfer.completed", Data: map[string]any{"id": id, "entry": e}})
		m.deps.Hub.Broadcast(sse.Event{Type: "browse.changed", Data: map[string]string{"dir": e.Path}})
		m.deps.Log.Info("upload finalized (folder)", "id", id, "path", e.Path)
		return
	}

	bin, err := os.Open(binPath)
	if err != nil {
		m.finalizeFailed(ctx, id, "open upload", err)
		return
	}
	hash, n, err := blob.HashReader(bin)
	bin.Close()
	if err != nil {
		m.finalizeFailed(ctx, id, "hash upload", err)
		return
	}

	existed, err := m.deps.Blob.Adopt(binPath, hash)
	if err != nil {
		m.finalizeFailed(ctx, id, "adopt blob", err)
		return
	}
	_ = existed // dedup is handled at the DB refcount layer

	f, err := m.deps.DB.CreateFile(ctx, name, kind, mimeType, n, hash)
	if err != nil {
		m.finalizeFailed(ctx, id, "create file row", err)
		return
	}

	if err := m.deps.DB.CompleteUploadSession(ctx, id, f.ID, n); err != nil {
		m.deps.Log.Warn("mark upload complete", "id", id, "err", err)
	}
	// Clean up the tus .info sidecar; the .bin is already gone (adopted).
	_ = os.Remove(binPath + ".info")

	dup := ""
	if existing, e := m.deps.DB.FileByHash(ctx, hash); e == nil && existing.ID != f.ID {
		dup = existing.ID
	}
	m.deps.Hub.Broadcast(sse.Event{Type: "transfer.completed", Data: map[string]any{
		"id": id, "file": f, "duplicate_of": dup,
	}})
	m.deps.Hub.Broadcast(sse.Event{Type: "file.created", Data: f})
	m.deps.Log.Info("upload finalized", "id", id, "file", f.ID, "hash", hash[:12], "dedup", existed)
}

func (m *Manager) finalizeFailed(ctx context.Context, id, stage string, err error) {
	m.deps.Log.Error("finalize upload failed", "id", id, "stage", stage, "err", err)
	_ = m.deps.DB.FailUploadSession(ctx, id)
	m.deps.Hub.Broadcast(sse.Event{Type: "transfer.failed", Data: map[string]any{
		"id": id, "stage": stage,
	}})
}

func (m *Manager) binPath(ev tushandler.HookEvent) string {
	if p := ev.Upload.Storage["Path"]; p != "" {
		return p
	}
	return m.deps.UploadDir + string(os.PathSeparator) + ev.Upload.ID
}

// --- metadata helpers -----------------------------------------------------

func metaOf(md map[string]string) (name, kind, mimeType string) {
	name = safepath.SanitizeFilename(firstNonEmpty(md["filename"], md["name"], md["fileName"]))
	kind = md["kind"]
	if kind != "screenshot" {
		kind = "file"
	}
	mimeType = strings.TrimSpace(firstNonEmpty(md["filetype"], md["type"], md["mime"]))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return name, kind, mimeType
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
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

// discardHandler is an x/exp/slog.Handler that drops everything; tusd logs
// verbosely at info and we surface our own upload lifecycle logs instead.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, xslog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, xslog.Record) error { return nil }
func (d discardHandler) WithAttrs([]xslog.Attr) xslog.Handler     { return d }
func (d discardHandler) WithGroup(string) xslog.Handler           { return d }

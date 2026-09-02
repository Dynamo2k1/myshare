// Package app wires MyShare's subsystems together and manages the server
// lifecycle: create the data layout, open the database, start the HTTP server,
// run background cleanup, and shut everything down cleanly on signal.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ranauzair/myshare/internal/api"
	"github.com/ranauzair/myshare/internal/auth"
	"github.com/ranauzair/myshare/internal/blob"
	"github.com/ranauzair/myshare/internal/config"
	"github.com/ranauzair/myshare/internal/diskusage"
	"github.com/ranauzair/myshare/internal/server"
	"github.com/ranauzair/myshare/internal/sse"
	"github.com/ranauzair/myshare/internal/store"
	"github.com/ranauzair/myshare/internal/uploads"
)

// Layout is the on-disk directory structure under the data dir.
type Layout struct {
	Root      string
	DBPath    string
	BlobDir   string
	UploadDir string
	TempDir   string
}

func layoutFor(dataDir string) Layout {
	return Layout{
		Root:      dataDir,
		DBPath:    filepath.Join(dataDir, "myshare.db"),
		BlobDir:   filepath.Join(dataDir, "blobs"),
		UploadDir: filepath.Join(dataDir, "uploads"),
		TempDir:   filepath.Join(dataDir, "tmp"),
	}
}

// App is a fully-wired, not-yet-listening MyShare instance.
type App struct {
	Cfg    config.Config
	Log    *slog.Logger
	Layout Layout

	db      *store.DB
	blob    *blob.Store
	hub     *sse.Hub
	authMgr *auth.Manager
	uploads *uploads.Manager
	handler http.Handler
	disk    diskusage.Usage
	started time.Time
}

// New builds the data layout and wires every subsystem, but does not bind a
// socket. Call Serve with an already-bound listener.
func New(ctx context.Context, cfg config.Config, log *slog.Logger, devProxy string) (*App, error) {
	start := time.Now()
	lay := layoutFor(cfg.DataDir)
	for _, d := range []string{lay.Root, lay.BlobDir, lay.UploadDir, lay.TempDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", d, err)
		}
	}

	du, derr := diskusage.Of(lay.Root)
	if derr != nil {
		log.Warn("could not stat data filesystem", "err", derr)
	}
	useWAL := !du.UnsafeWAL
	if du.UnsafeWAL {
		log.Warn("data directory is on a filesystem where SQLite WAL is unsafe; "+
			"using rollback-journal mode instead",
			"fs_type", du.FSType, "path", lay.Root)
	}

	db, err := store.Open(ctx, lay.DBPath, useWAL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	blobStore, err := blob.New(lay.BlobDir)
	if err != nil {
		db.Close()
		return nil, err
	}

	hub := sse.NewHub(log)
	authMgr := auth.New(db, cfg.Auth)

	apiSvc := &api.API{
		DB: db, Blob: blobStore, Hub: hub, Cfg: cfg, Log: log, TempDir: lay.TempDir,
	}

	upMgr, err := uploads.New(uploads.Deps{
		DB: db, Blob: blobStore, Hub: hub, Log: log,
		UploadDir:   lay.UploadDir,
		MaxFileSize: cfg.MaxFileSize,
		MaxStorage:  cfg.MaxStorage,
		BasePath:    "/api/tus/",
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("upload subsystem: %w", err)
	}

	h := server.New(server.Options{
		Cfg: cfg, API: apiSvc, Auth: authMgr, Hub: hub, DB: db,
		TusHandler: upMgr.Handler(), Log: log, DevProxy: devProxy,
	})

	return &App{
		Cfg: cfg, Log: log, Layout: lay,
		db: db, blob: blobStore, hub: hub, authMgr: authMgr,
		uploads: upMgr, handler: h, disk: du, started: start,
	}, nil
}

// Auth exposes the auth manager (for the set-password CLI path).
func (a *App) Auth() *auth.Manager { return a.authMgr }

// DB exposes the database (for CLI subcommands).
func (a *App) DB() *store.DB { return a.db }

// Disk returns the data filesystem usage sampled at startup.
func (a *App) Disk() diskusage.Usage { return a.disk }

// Serve runs the HTTP server on ln until ctx is cancelled, then shuts down
// gracefully. It closes owned resources (DB, upload manager) before returning.
func (a *App) Serve(ctx context.Context, ln net.Listener) error {
	defer a.db.Close()
	defer a.uploads.Shutdown()

	srv := &http.Server{
		Handler:           a.handler,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: large downloads and long uploads must not be cut off.
	}

	cleanupCtx, stopCleanup := context.WithCancel(ctx)
	defer stopCleanup()
	go a.runCleanup(cleanupCtx)

	errCh := make(chan error, 1)
	go func() {
		if a.Cfg.TLS {
			cert, key, err := a.ensureTLSCert()
			if err != nil {
				errCh <- fmt.Errorf("tls cert: %w", err)
				return
			}
			errCh <- srv.ServeTLS(ln, cert, key)
		} else {
			errCh <- srv.Serve(ln)
		}
	}()

	a.Log.Info("ready", "addr", ln.Addr().String(), "startup", time.Since(a.started).Round(time.Millisecond).String())

	select {
	case <-ctx.Done():
		a.Log.Info("shutting down")
		// Close the SSE streams first — http.Server.Shutdown waits for active
		// requests, and an event stream never ends on its own.
		a.hub.Shutdown()

		shCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := srv.Shutdown(shCtx)
		if errors.Is(err, context.DeadlineExceeded) {
			// A long download or upload is still in flight. Don't hang on it.
			a.Log.Warn("forcing shutdown (a transfer was still active)")
			_ = srv.Close()
			err = nil
		}
		if err == nil {
			a.Log.Info("stopped cleanly")
		}
		return err
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

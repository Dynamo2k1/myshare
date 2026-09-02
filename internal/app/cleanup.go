package app

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// runCleanup periodically removes abandoned uploads, expired/revoked shares,
// stale temp files, and orphaned blobs. It never touches a live file.
func (a *App) runCleanup(ctx context.Context) {
	interval := a.Cfg.CleanupInterval
	if interval <= 0 {
		return
	}
	// Run once shortly after start, then on the interval.
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			a.cleanupOnce(ctx)
			timer.Reset(interval)
		}
	}
}

func (a *App) cleanupOnce(ctx context.Context) {
	// 1. Abandoned tus uploads: active sessions untouched for 24h.
	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	if ids, err := a.db.StaleUploadSessionIDs(ctx, cutoff); err == nil {
		for _, id := range ids {
			_ = os.Remove(filepath.Join(a.Layout.UploadDir, id))
			_ = os.Remove(filepath.Join(a.Layout.UploadDir, id+".info"))
			_ = a.db.FailUploadSession(ctx, id)
		}
		if len(ids) > 0 {
			a.Log.Info("cleanup: removed abandoned uploads", "count", len(ids))
		}
	}

	// 2. Expired / revoked shares.
	if ids, err := a.db.ExpiredShareIDs(ctx, 500); err == nil {
		for _, id := range ids {
			_ = a.db.DeleteShare(ctx, id)
		}
		if len(ids) > 0 {
			a.Log.Info("cleanup: removed expired shares", "count", len(ids))
		}
	}

	// 3. Orphaned blob rows (refcount <= 0) and their bytes.
	if hashes, err := a.db.OrphanBlobHashes(ctx, 1000); err == nil {
		for _, h := range hashes {
			_ = a.blob.Remove(h)
			_ = a.db.DeleteBlobRow(ctx, h)
		}
		if len(hashes) > 0 {
			a.Log.Info("cleanup: removed orphan blobs", "count", len(hashes))
		}
	}

	// 4. Stale temp files older than 6h (interrupted direct uploads).
	if entries, err := os.ReadDir(a.Layout.TempDir); err == nil {
		tcut := time.Now().Add(-6 * time.Hour)
		for _, e := range entries {
			info, err := e.Info()
			if err == nil && info.ModTime().Before(tcut) {
				_ = os.Remove(filepath.Join(a.Layout.TempDir, e.Name()))
			}
		}
	}

	// 5. Storage warning.
	if a.Cfg.MaxStorage > 0 {
		if st, err := a.db.Stats(ctx); err == nil {
			pct := float64(st.BlobBytes) / float64(a.Cfg.MaxStorage) * 100
			if pct >= 90 {
				a.Log.Warn("storage almost full", "used_pct", int(pct),
					"used", st.BlobBytes, "limit", a.Cfg.MaxStorage)
			}
		}
	}
}

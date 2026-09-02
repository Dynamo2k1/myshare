package app

import (
	"context"
	"time"

	"github.com/dynamo2k1/myshare/internal/sse"
)

// watchServedDir polls the served directory tree in directory mode and pushes a
// "browse.changed" event when anything is added, removed or renamed from
// outside MyShare, so open tabs refresh without a manual reload.
func (a *App) watchServedDir(ctx context.Context) {
	prev := map[string]string{}
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			changed := false
			seen := map[string]bool{}
			// Fingerprint the root and any directory currently cached; cheap for
			// typical folders. A deep tree is bounded by only walking one level
			// per tick from the root plus already-known subdirs.
			for _, dir := range append([]string{""}, keys(prev)...) {
				seen[dir] = true
				fp, err := a.browser.FingerprintDir(dir)
				if err != nil {
					if prev[dir] != "" {
						delete(prev, dir)
						changed = true
					}
					continue
				}
				if prev[dir] != fp {
					prev[dir] = fp
					changed = true
				}
			}
			if changed {
				a.hub.Broadcast(sse.Event{Type: "browse.changed", Data: map[string]bool{"external": true}})
			}
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

package api

import (
	"net/http"
	"time"

	"github.com/dynamo2k1/myshare/internal/sse"
)

// The scratch is a single shared Markdown document that every device sees and
// edits live — no "save" button. It is stored as one settings row and pushed
// over SSE on every change (echo-suppressed for the device that made the edit).

const scratchKey = "scratch.markdown"
const maxScratchBytes = 4 << 20 // 4 MiB

type scratchDoc struct {
	Content   string `json:"content"`
	UpdatedAt int64  `json:"updated_at"`
}

func (a *API) getScratch(w http.ResponseWriter, r *http.Request) {
	content, err := a.DB.GetSetting(r.Context(), scratchKey)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	ts := int64(0)
	if v, _ := a.DB.GetSetting(r.Context(), scratchKey+".at"); v != "" {
		ts, _ = parseUnix(v)
	}
	writeJSON(w, http.StatusOK, scratchDoc{Content: content, UpdatedAt: ts})
}

func (a *API) putScratch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &req, maxScratchBytes+4096); err != nil {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "Invalid request body.", nil)
		return
	}
	if len(req.Content) > maxScratchBytes {
		a.fail(w, r, http.StatusRequestEntityTooLarge, "too_large", "That document is too large.", nil)
		return
	}
	now := time.Now().Unix()
	if err := a.DB.SetSetting(r.Context(), scratchKey, req.Content); err != nil {
		a.failStore(w, r, err)
		return
	}
	_ = a.DB.SetSetting(r.Context(), scratchKey+".at", itoa64(now))

	doc := scratchDoc{Content: req.Content, UpdatedAt: now}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "scratch.updated", Data: doc})
	writeJSON(w, http.StatusOK, doc)
}

func parseUnix(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return n, nil
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

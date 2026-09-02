package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/dynamo2k1/myshare/internal/sse"
)

// maxTextBytes bounds a single clipboard/snippet/note body. Generous but finite
// so a client cannot exhaust memory or the row size.
const maxTextBytes = 4 << 20 // 4 MiB

var urlRe = regexp.MustCompile(`^\s*https?://[^\s]+\s*$`)

// detectFormat classifies clipboard content for nicer rendering client-side.
func detectFormat(s string) string {
	t := strings.TrimSpace(s)
	switch {
	case urlRe.MatchString(t):
		return "url"
	case strings.Contains(t, "```") || strings.HasPrefix(t, "#") || strings.Contains(t, "\n- "):
		return "markdown"
	case looksLikeCode(t):
		return "code"
	default:
		return "text"
	}
}

func looksLikeCode(s string) bool {
	hits := 0
	for _, tok := range []string{";", "{", "}", "()", "=>", "def ", "func ", "import ", "const ", "  "} {
		if strings.Contains(s, tok) {
			hits++
		}
	}
	return hits >= 2 && strings.Contains(s, "\n")
}

// --- clipboard ------------------------------------------------------------

type clipboardReq struct {
	Content string  `json:"content"`
	Format  *string `json:"format"`
	Pinned  *bool   `json:"pinned"`
}

func (a *API) listClipboard(w http.ResponseWriter, r *http.Request) {
	page, err := a.DB.ListClipboard(r.Context(), r.URL.Query().Get("q"),
		queryInt(r, "limit", 50), queryInt(r, "offset", 0))
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *API) createClipboard(w http.ResponseWriter, r *http.Request) {
	var req clipboardReq
	if err := decodeJSON(r, &req, maxTextBytes+4096); err != nil {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "Invalid request body.", nil)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "Nothing to save.", nil)
		return
	}
	if len(req.Content) > maxTextBytes {
		a.fail(w, r, http.StatusRequestEntityTooLarge, "too_large", "That text is too large for the clipboard.", nil)
		return
	}
	format := ""
	if req.Format != nil {
		format = *req.Format
	}
	if format == "" {
		format = detectFormat(req.Content)
	}
	it, err := a.DB.CreateClipboard(r.Context(), req.Content, format)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "clipboard.created", Data: it})
	writeJSON(w, http.StatusCreated, it)
}

func (a *API) patchClipboard(w http.ResponseWriter, r *http.Request) {
	var req clipboardReq
	if err := decodeJSON(r, &req, maxTextBytes+4096); err != nil {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "Invalid request body.", nil)
		return
	}
	var contentPtr *string
	if strings.TrimSpace(req.Content) != "" || req.Content != "" {
		if len(req.Content) > maxTextBytes {
			a.fail(w, r, http.StatusRequestEntityTooLarge, "too_large", "That text is too large.", nil)
			return
		}
		contentPtr = &req.Content
	}
	it, err := a.DB.UpdateClipboard(r.Context(), chi.URLParam(r, "id"), contentPtr, req.Format, req.Pinned)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "clipboard.updated", Data: it})
	writeJSON(w, http.StatusOK, it)
}

func (a *API) deleteClipboard(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.DB.DeleteClipboard(r.Context(), id); err != nil {
		a.failStore(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "clipboard.deleted", Data: map[string]string{"id": id}})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) clearClipboard(w http.ResponseWriter, r *http.Request) {
	if err := a.DB.ClearClipboard(r.Context()); err != nil {
		a.failStore(w, r, err)
		return
	}
	a.Hub.Broadcast(sse.Event{Type: "clipboard.cleared"})
	w.WriteHeader(http.StatusNoContent)
}

// --- snippets -----------------------------------------------------------

type snippetReq struct {
	Title    *string `json:"title"`
	Content  string  `json:"content"`
	Language *string `json:"language"`
	Pinned   *bool   `json:"pinned"`
}

func (a *API) listSnippets(w http.ResponseWriter, r *http.Request) {
	page, err := a.DB.ListSnippets(r.Context(), r.URL.Query().Get("q"),
		queryInt(r, "limit", 50), queryInt(r, "offset", 0))
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *API) getSnippet(w http.ResponseWriter, r *http.Request) {
	s, err := a.DB.GetSnippet(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (a *API) createSnippet(w http.ResponseWriter, r *http.Request) {
	var req snippetReq
	if err := decodeJSON(r, &req, maxTextBytes+4096); err != nil {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "Invalid request body.", nil)
		return
	}
	if len(req.Content) > maxTextBytes {
		a.fail(w, r, http.StatusRequestEntityTooLarge, "too_large", "That snippet is too large.", nil)
		return
	}
	title, lang := "", ""
	if req.Title != nil {
		title = *req.Title
	}
	if req.Language != nil {
		lang = *req.Language
	}
	s, err := a.DB.CreateSnippet(r.Context(), title, req.Content, lang)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "snippet.created", Data: s})
	writeJSON(w, http.StatusCreated, s)
}

func (a *API) patchSnippet(w http.ResponseWriter, r *http.Request) {
	var req snippetReq
	if err := decodeJSON(r, &req, maxTextBytes+4096); err != nil {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "Invalid request body.", nil)
		return
	}
	var contentPtr *string
	if req.Content != "" {
		if len(req.Content) > maxTextBytes {
			a.fail(w, r, http.StatusRequestEntityTooLarge, "too_large", "That snippet is too large.", nil)
			return
		}
		contentPtr = &req.Content
	}
	s, err := a.DB.UpdateSnippet(r.Context(), chi.URLParam(r, "id"), req.Title, contentPtr, req.Language, req.Pinned)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "snippet.updated", Data: s})
	writeJSON(w, http.StatusOK, s)
}

func (a *API) deleteSnippet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.DB.DeleteSnippet(r.Context(), id); err != nil {
		a.failStore(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "snippet.deleted", Data: map[string]string{"id": id}})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) duplicateSnippet(w http.ResponseWriter, r *http.Request) {
	src, err := a.DB.GetSnippet(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	title := strings.TrimSpace(src.Title)
	if title == "" {
		title = "Copy"
	} else {
		title += " (copy)"
	}
	s, err := a.DB.CreateSnippet(r.Context(), title, src.Content, src.Language)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "snippet.created", Data: s})
	writeJSON(w, http.StatusCreated, s)
}

// --- notes ------------------------------------------------------------------

type noteReq struct {
	Title   *string `json:"title"`
	Content *string `json:"content"`
	Pinned  *bool   `json:"pinned"`
}

func (a *API) listNotes(w http.ResponseWriter, r *http.Request) {
	page, err := a.DB.ListNotes(r.Context(), r.URL.Query().Get("q"),
		queryInt(r, "limit", 50), queryInt(r, "offset", 0))
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *API) getNote(w http.ResponseWriter, r *http.Request) {
	n, err := a.DB.GetNote(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (a *API) createNote(w http.ResponseWriter, r *http.Request) {
	var req noteReq
	if err := decodeJSON(r, &req, maxTextBytes+4096); err != nil {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "Invalid request body.", nil)
		return
	}
	title, content := "", ""
	if req.Title != nil {
		title = *req.Title
	}
	if req.Content != nil {
		content = *req.Content
	}
	if len(content) > maxTextBytes {
		a.fail(w, r, http.StatusRequestEntityTooLarge, "too_large", "That note is too large.", nil)
		return
	}
	n, err := a.DB.CreateNote(r.Context(), title, content)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "note.created", Data: n})
	writeJSON(w, http.StatusCreated, n)
}

func (a *API) patchNote(w http.ResponseWriter, r *http.Request) {
	var req noteReq
	if err := decodeJSON(r, &req, maxTextBytes+4096); err != nil {
		a.fail(w, r, http.StatusBadRequest, "bad_request", "Invalid request body.", nil)
		return
	}
	if req.Content != nil && len(*req.Content) > maxTextBytes {
		a.fail(w, r, http.StatusRequestEntityTooLarge, "too_large", "That note is too large.", nil)
		return
	}
	n, err := a.DB.UpdateNote(r.Context(), chi.URLParam(r, "id"), req.Title, req.Content, req.Pinned)
	if err != nil {
		a.failStore(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "note.updated", Data: n})
	writeJSON(w, http.StatusOK, n)
}

func (a *API) deleteNote(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.DB.DeleteNote(r.Context(), id); err != nil {
		a.failStore(w, r, err)
		return
	}
	a.Hub.BroadcastExcept(originID(r), sse.Event{Type: "note.deleted", Data: map[string]string{"id": id}})
	w.WriteHeader(http.StatusNoContent)
}

// Package sse implements a minimal server-sent-events hub for pushing
// change notifications to every connected browser. It is deliberately one-way
// (server -> client); clients mutate state through the REST API.
package sse

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// clientBuffer bounds how many undelivered events a slow client may accumulate
// before it is force-resynced. It is intentionally small: the client reacts to
// a "resync" by refetching, so we never need an unbounded queue.
const clientBuffer = 64

// Event is a single notification. Type is the SSE event name; Data is
// JSON-marshalled as the payload.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

type client struct {
	id     uint64
	ch     chan Event
	origin string // client-supplied id, so an action's originator can skip its own echo
}

// Hub fans out events to all connected SSE clients.
type Hub struct {
	mu      sync.RWMutex
	clients map[uint64]*client
	nextID  atomic.Uint64
	log     *slog.Logger

	done      chan struct{} // closed on Shutdown; ServeHTTP loops watch it
	closeOnce sync.Once
}

// NewHub returns a ready hub.
func NewHub(log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{
		clients: make(map[uint64]*client),
		log:     log,
		done:    make(chan struct{}),
	}
}

// Shutdown signals every connected client's ServeHTTP loop to return
// immediately, so the HTTP server's graceful shutdown isn't blocked waiting for
// long-lived event streams to end.
func (h *Hub) Shutdown() {
	h.closeOnce.Do(func() { close(h.done) })
}

// Broadcast delivers ev to every connected client. Clients whose buffer is full
// are sent a "resync" instead and expected to refetch. Never blocks.
func (h *Hub) Broadcast(ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		select {
		case c.ch <- ev:
		default:
			select {
			case c.ch <- Event{Type: "resync"}:
			default:
			}
		}
	}
}

// BroadcastExcept is Broadcast but skips clients whose origin matches originID
// (the device that caused the change and has already updated its own UI).
func (h *Hub) BroadcastExcept(originID string, ev Event) {
	if originID == "" {
		h.Broadcast(ev)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		if c.origin == originID {
			continue
		}
		select {
		case c.ch <- ev:
		default:
			select {
			case c.ch <- Event{Type: "resync"}:
			default:
			}
		}
	}
}

// Count returns the number of connected clients.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ServeHTTP streams events to one client until the request context is done.
// The query parameter "client" is remembered as the origin id for echo
// suppression.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	c := &client{
		id:     h.nextID.Add(1),
		ch:     make(chan Event, clientBuffer),
		origin: r.URL.Query().Get("client"),
	}
	h.mu.Lock()
	h.clients[c.id] = c
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, c.id)
		h.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, "retry: 3000\n\n")
	fmt.Fprintf(w, "event: hello\ndata: {\"ok\":true}\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.done: // server is shutting down
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev := <-c.ch:
			payload, err := json.Marshal(ev.Data)
			if err != nil {
				h.log.Warn("sse marshal", "err", err)
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/dynamo2k1/myshare/internal/auth"
	"github.com/dynamo2k1/myshare/internal/config"
)

type authRoutes struct {
	mgr *auth.Manager
	cfg config.Config
	log *slog.Logger
}

func (h *authRoutes) status(w http.ResponseWriter, r *http.Request) {
	hasPw, _ := h.mgr.HasPassword(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":       h.mgr.Enabled(),
		"password_set":  hasPw,
		"authenticated": h.mgr.Authenticated(r),
	})
}

func (h *authRoutes) login(w http.ResponseWriter, r *http.Request) {
	if !h.mgr.Enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request."})
		return
	}
	token, err := h.mgr.Login(r.Context(), r, body.Password)
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "Too many attempts. Wait a few minutes and try again."})
		return
	case errors.Is(err, auth.ErrBadCredentials):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Incorrect password."})
		return
	case err != nil:
		h.log.Error("login", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Login failed."})
		return
	}
	http.SetCookie(w, auth.SessionCookie(token, h.cfg.TLS))
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

func (h *authRoutes) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("myshare_session"); err == nil {
		h.mgr.Logout(c.Value)
	}
	http.SetCookie(w, auth.ClearCookie())
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

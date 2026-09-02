// Package auth provides optional single-password authentication: argon2id
// password hashing, opaque session cookies, and login rate limiting.
//
// When disabled (the default), Middleware is a pass-through. When enabled, every
// route it wraps requires a valid session cookie; unauthenticated API calls get
// 401 and unauthenticated page loads are allowed through so the SPA can render
// its own login screen and call POST /api/auth/login.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	sessionCookie = "myshare_session"
	sessionTTL    = 30 * 24 * time.Hour
	loginWindow   = 15 * time.Minute
	loginMaxTries = 10
)

// Store is the interface auth needs from the database: a place to keep the
// password hash and (optionally) nothing else. Sessions are kept in memory.
type Store interface {
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
	DeleteSetting(ctx context.Context, key string) error
}

const settingKey = "auth.password_argon2id"

// Manager holds auth state.
type Manager struct {
	store   Store
	enabled bool

	mu       sync.Mutex
	sessions map[string]time.Time // token -> expiry
	attempts map[string]*bucket   // client ip -> rate bucket
}

type bucket struct {
	count int
	reset time.Time
}

// New returns a Manager. enabled mirrors config.Auth.
func New(store Store, enabled bool) *Manager {
	m := &Manager{
		store:    store,
		enabled:  enabled,
		sessions: make(map[string]time.Time),
		attempts: make(map[string]*bucket),
	}
	go m.gc()
	return m
}

// Enabled reports whether authentication is enforced.
func (m *Manager) Enabled() bool { return m.enabled }

// HasPassword reports whether a password has been configured.
func (m *Manager) HasPassword(ctx context.Context) (bool, error) {
	v, err := m.store.GetSetting(ctx, settingKey)
	if err != nil {
		return false, err
	}
	return v != "", nil
}

// SetPassword stores a new argon2id hash for pw. An empty pw clears it.
func (m *Manager) SetPassword(ctx context.Context, pw string) error {
	if pw == "" {
		return m.store.DeleteSetting(ctx, settingKey)
	}
	if len(pw) < 6 {
		return errors.New("password must be at least 6 characters")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	// Parameters: 3 passes, 64 MiB, 4 lanes — OWASP-recommended baseline.
	key := argon2.IDKey([]byte(pw), salt, 3, 64*1024, 4, 32)
	enc := fmt.Sprintf("argon2id$v=19$m=65536,t=3,p=4$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
	return m.store.SetSetting(ctx, settingKey, enc)
}

// verify checks pw against the stored hash.
func (m *Manager) verify(ctx context.Context, pw string) (bool, error) {
	enc, err := m.store.GetSetting(ctx, settingKey)
	if err != nil || enc == "" {
		return false, err
	}
	parts := strings.Split(enc, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false, errors.New("corrupt password hash")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(pw), salt, 3, 64*1024, 4, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// Login verifies credentials (rate-limited per client IP) and, on success,
// returns a Set-Cookie-ready session token.
func (m *Manager) Login(ctx context.Context, r *http.Request, pw string) (token string, err error) {
	ip := clientIP(r)
	if !m.allowAttempt(ip) {
		return "", ErrRateLimited
	}
	ok, err := m.verify(ctx, pw)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrBadCredentials
	}
	m.resetAttempts(ip)

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token = hex.EncodeToString(raw)
	m.mu.Lock()
	m.sessions[token] = time.Now().Add(sessionTTL)
	m.mu.Unlock()
	return token, nil
}

// Logout invalidates a session token.
func (m *Manager) Logout(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

// Errors returned by Login.
var (
	ErrBadCredentials = errors.New("invalid password")
	ErrRateLimited    = errors.New("too many attempts; try again later")
)

func (m *Manager) validSession(token string) bool {
	if token == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(m.sessions, token)
		return false
	}
	return true
}

// SessionCookie builds the Set-Cookie for a freshly issued token. secure marks
// it Secure (used when serving over TLS).
func SessionCookie(token string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionTTL),
		MaxAge:   int(sessionTTL.Seconds()),
	}
}

// ClearCookie builds a Set-Cookie that removes the session.
func ClearCookie() *http.Cookie {
	return &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/",
		HttpOnly: true, MaxAge: -1, Expires: time.Unix(0, 0),
	}
}

type ctxKey int

const authedKey ctxKey = 0

// APIMiddleware guards API routes: when auth is enabled and the request has no
// valid session, it responds 401 and does not call next.
func (m *Manager) APIMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.enabled {
			next.ServeHTTP(w, r)
			return
		}
		if m.authenticated(r) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authedKey, true)))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"authentication required","code":"unauthorized"}`))
	})
}

func (m *Manager) authenticated(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	return m.validSession(c.Value)
}

// Authenticated reports whether r carries a valid session (or auth is off).
func (m *Manager) Authenticated(r *http.Request) bool {
	return !m.enabled || m.authenticated(r)
}

// --- rate limiting ---------------------------------------------------------

func (m *Manager) allowAttempt(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.attempts[ip]
	now := time.Now()
	if b == nil || now.After(b.reset) {
		m.attempts[ip] = &bucket{count: 1, reset: now.Add(loginWindow)}
		return true
	}
	if b.count >= loginMaxTries {
		return false
	}
	b.count++
	return true
}

func (m *Manager) resetAttempts(ip string) {
	m.mu.Lock()
	delete(m.attempts, ip)
	m.mu.Unlock()
}

func (m *Manager) gc() {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		m.mu.Lock()
		for tok, exp := range m.sessions {
			if now.After(exp) {
				delete(m.sessions, tok)
			}
		}
		for ip, b := range m.attempts {
			if now.After(b.reset) {
				delete(m.attempts, ip)
			}
		}
		m.mu.Unlock()
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

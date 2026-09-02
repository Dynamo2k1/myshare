package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ranauzair/myshare/internal/api"
	"github.com/ranauzair/myshare/internal/auth"
	"github.com/ranauzair/myshare/internal/blob"
	"github.com/ranauzair/myshare/internal/config"
	"github.com/ranauzair/myshare/internal/sse"
	"github.com/ranauzair/myshare/internal/store"
)

func newServer(t *testing.T, cfg config.Config) (*httptest.Server, *store.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(context.Background(), filepath.Join(dir, "t.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	bs, err := blob.New(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := sse.NewHub(log)
	cfg.DataDir = dir
	a := &api.API{DB: db, Blob: bs, Hub: hub, Cfg: cfg, Log: log, TempDir: filepath.Join(dir, "tmp")}
	am := auth.New(db, cfg.Auth)

	h := New(Options{Cfg: cfg, API: a, Auth: am, Hub: hub, DB: db, Log: log})
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv, db
}

func upload(t *testing.T, base, name, body string) *http.Response {
	t.Helper()
	var buf strings.Builder
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write([]byte(body))
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, base+"/api/files", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestFilesRoundTripAndTraversalNames(t *testing.T) {
	srv, _ := newServer(t, config.Defaults())

	// A hostile filename must be stored sanitised, never as a path.
	resp := upload(t, srv.URL, "../../../../etc/passwd", "root:x:0:0")
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status %d: %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	list, err := http.Get(srv.URL + "/api/files")
	if err != nil {
		t.Fatal(err)
	}
	defer list.Body.Close()
	b, _ := io.ReadAll(list.Body)
	body := string(b)
	if strings.Contains(body, "etc/passwd") || strings.Contains(body, "..") {
		t.Fatalf("stored name leaked a path: %s", body)
	}
	if !strings.Contains(body, `"name":"passwd"`) {
		t.Fatalf("expected sanitised name 'passwd', got: %s", body)
	}
}

func TestRawDownloadHeadersAndRange(t *testing.T) {
	srv, db := newServer(t, config.Defaults())
	_ = upload(t, srv.URL, "note.txt", "hello range world").Body.Close()

	page, _ := db.ListFiles(context.Background(), store.FileListOptions{})
	id := page.Items[0].ID

	// Full download: attachment + nosniff for text/plain? text/plain is on the
	// inline allowlist, so disposition is inline but nosniff still set.
	resp, err := http.Get(srv.URL + "/api/files/" + id + "/raw")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff on download")
	}
	if !strings.Contains(resp.Header.Get("Content-Disposition"), "filename*=UTF-8''note.txt") {
		t.Errorf("bad Content-Disposition: %q", resp.Header.Get("Content-Disposition"))
	}
	resp.Body.Close()

	// Range request.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/files/"+id+"/raw", nil)
	req.Header.Set("Range", "bytes=0-4")
	rr, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Body.Close()
	if rr.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", rr.StatusCode)
	}
	got, _ := io.ReadAll(rr.Body)
	if string(got) != "hello" {
		t.Errorf("range body = %q, want %q", got, "hello")
	}
}

func TestUploadedHTMLServedAsAttachment(t *testing.T) {
	srv, db := newServer(t, config.Defaults())
	_ = upload(t, srv.URL, "evil.html", "<script>alert(1)</script>").Body.Close()

	page, _ := db.ListFiles(context.Background(), store.FileListOptions{})
	id := page.Items[0].ID
	resp, err := http.Get(srv.URL + "/api/files/" + id + "/raw")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("HTML upload must download, not render inline: %q",
			resp.Header.Get("Content-Disposition"))
	}
}

func TestSecurityHeadersPresent(t *testing.T) {
	srv, _ := newServer(t, config.Defaults())
	resp, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options"} {
		if resp.Header.Get(h) == "" {
			t.Errorf("missing security header %s", h)
		}
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Error("CSP not restrictive")
	}
}

func TestCSRFOriginCheck(t *testing.T) {
	srv, _ := newServer(t, config.Defaults())

	// Cross-origin POST is rejected.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/clipboard",
		strings.NewReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST status = %d, want 403", resp.StatusCode)
	}

	// Same-origin POST is allowed.
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/clipboard",
		strings.NewReader(`{"content":"ok"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Origin", srv.URL)
	resp2, _ := http.DefaultClient.Do(req2)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("same-origin POST status = %d, want 201", resp2.StatusCode)
	}

	// No Origin (CLI/curl) is allowed.
	req3, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/clipboard",
		strings.NewReader(`{"content":"cli"}`))
	req3.Header.Set("Content-Type", "application/json")
	resp3, _ := http.DefaultClient.Do(req3)
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusCreated {
		t.Fatalf("no-origin POST status = %d, want 201", resp3.StatusCode)
	}
}

func TestAuthGatesAPIWhenEnabled(t *testing.T) {
	cfg := config.Defaults()
	cfg.Auth = true
	srv, db := newServer(t, cfg)

	// No password set yet, no session -> API is 401.
	resp, _ := http.Get(srv.URL + "/api/files")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("guarded API without session = %d, want 401", resp.StatusCode)
	}

	// Set a password, log in, get a cookie, then the API works.
	am := auth.New(db, true)
	if err := am.SetPassword(context.Background(), "hunter2"); err != nil {
		t.Fatal(err)
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	lr, _ := client.Post(srv.URL+"/api/auth/login", "application/json",
		strings.NewReader(`{"password":"hunter2"}`))
	lr.Body.Close()
	if lr.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", lr.StatusCode)
	}
	ar, _ := client.Get(srv.URL + "/api/files")
	ar.Body.Close()
	if ar.StatusCode != http.StatusOK {
		t.Fatalf("authenticated API = %d, want 200", ar.StatusCode)
	}

	// Wrong password is 401.
	wr, _ := http.Post(srv.URL+"/api/auth/login", "application/json",
		strings.NewReader(`{"password":"nope"}`))
	wr.Body.Close()
	if wr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d, want 401", wr.StatusCode)
	}
}

func TestShareLinkFullLifecycle(t *testing.T) {
	srv, db := newServer(t, config.Defaults())
	_ = upload(t, srv.URL, "shared.txt", "secret contents").Body.Close()
	page, _ := db.ListFiles(context.Background(), store.FileListOptions{})
	id := page.Items[0].ID

	// Create a one-time share.
	cr, _ := http.Post(srv.URL+"/api/shares", "application/json",
		strings.NewReader(`{"file_id":"`+id+`","one_time":true}`))
	var created struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	readJSON(t, cr, &created)
	if created.URL == "" {
		t.Fatal("share URL missing")
	}

	// First fetch works and returns the bytes.
	f1, _ := http.Get(created.URL)
	b1, _ := io.ReadAll(f1.Body)
	f1.Body.Close()
	if f1.StatusCode != http.StatusOK || string(b1) != "secret contents" {
		t.Fatalf("first share fetch: status=%d body=%q", f1.StatusCode, b1)
	}

	// Second fetch: one-time link is now revoked -> gone.
	f2, _ := http.Get(created.URL)
	f2.Body.Close()
	if f2.StatusCode != http.StatusGone {
		t.Fatalf("second one-time fetch = %d, want 410", f2.StatusCode)
	}

	// A bogus token 404s and never touches the filesystem.
	bad, _ := http.Get(srv.URL + "/s/not-a-real-token-................................")
	bad.Body.Close()
	if bad.StatusCode != http.StatusNotFound {
		t.Fatalf("bogus token = %d, want 404", bad.StatusCode)
	}
}

func readJSON(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
}

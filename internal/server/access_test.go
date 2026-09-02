package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAccessControl(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cases := []struct {
		mode   string
		ip     string
		expect int
	}{
		{"local", "127.0.0.1", 200},
		{"local", "::1", 200},
		{"local", "192.168.1.5", 403},
		{"local", "8.8.8.8", 403},

		{"lan", "127.0.0.1", 200},
		{"lan", "10.0.0.9", 200},
		{"lan", "192.168.18.130", 200},
		{"lan", "172.16.4.4", 200},
		{"lan", "169.254.1.2", 200},
		{"lan", "fe80::1", 200},
		{"lan", "8.8.8.8", 403},
		{"lan", "1.1.1.1", 403},
		{"lan", "2001:4860:4860::8888", 403},

		{"public", "8.8.8.8", 200},
		{"public", "192.168.1.5", 200},
		{"public", "127.0.0.1", 200},
	}

	for _, c := range cases {
		h := accessControl(c.mode)(ok)
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		req.RemoteAddr = net.JoinHostPort(c.ip, "50000")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.expect {
			t.Errorf("access=%s ip=%s -> %d, want %d", c.mode, c.ip, rec.Code, c.expect)
		}
	}
}

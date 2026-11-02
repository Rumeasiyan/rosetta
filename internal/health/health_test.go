package health

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	tests := []struct {
		name   string
		method string
		check  error
		status int
		body   string
	}{
		{"healthy", http.MethodGet, nil, http.StatusOK, "ok"},
		{"database down", http.MethodGet, errors.New("down"), http.StatusServiceUnavailable, "unhealthy"},
		{"other method", http.MethodPost, nil, http.StatusMethodNotAllowed, "method not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := Handler(func(context.Context) error { return tt.check }, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, Path, nil))
			if rec.Code != tt.status || strings.TrimSpace(rec.Body.String()) != tt.body {
				t.Fatalf("got %d %q, want %d %q", rec.Code, rec.Body.String(), tt.status, tt.body)
			}
		})
	}
}

func TestHandlerLogsReasonButNeverSendsIt(t *testing.T) {
	const reason = "dial tcp db.internal:5432: password authentication failed for user rosetta"
	var logs bytes.Buffer
	h := Handler(func(context.Context) error { return errors.New(reason) }, slog.New(slog.NewTextHandler(&logs, nil)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, Path, nil))
	if strings.Contains(rec.Body.String(), "db.internal") {
		t.Fatalf("body leaks the reason: %q", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "db.internal") {
		t.Fatalf("reason not logged: %q", logs.String())
	}
}

func TestProbe(t *testing.T) {
	healthy := httptest.NewServer(Handler(func(context.Context) error { return nil }, nil))
	defer healthy.Close()
	if err := Probe(context.Background(), healthy.URL+Path); err != nil {
		t.Fatalf("healthy service: %v", err)
	}
	sick := httptest.NewServer(Handler(func(context.Context) error { return errors.New("down") }, nil))
	defer sick.Close()
	if err := Probe(context.Background(), sick.URL+Path); err == nil {
		t.Fatal("unhealthy service passed the probe")
	}
	if err := Probe(context.Background(), "http://127.0.0.1:1"+Path); err == nil {
		t.Fatal("nothing listening passed the probe")
	}
}

func TestLoopbackURL(t *testing.T) {
	if got := LoopbackURL(8080); got != "http://127.0.0.1:8080/healthz" {
		t.Fatalf("LoopbackURL(8080) = %q", got)
	}
}

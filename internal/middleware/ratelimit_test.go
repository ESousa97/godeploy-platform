package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestWebhookRateLimit_AllowsUnderBurst(t *testing.T) {
	var hits atomic.Int32
	h := WebhookRateLimit(1000, 10, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 5; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
		req.RemoteAddr = "192.0.2.1:12345"
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: status %d body %q", i, rr.Code, rr.Body.String())
		}
	}
	if hits.Load() != 5 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestWebhookRateLimit_DefaultsWhenInvalid(t *testing.T) {
	h := WebhookRateLimit(0, 0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	req.RemoteAddr = "192.0.2.2:1"
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusTeapot {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestClientIPKey(t *testing.T) {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:9999"
	if got := clientIPKey(req); got != "203.0.113.5" {
		t.Fatalf("got %q", got)
	}
	req.RemoteAddr = "nohostport"
	if got := clientIPKey(req); got != "nohostport" {
		t.Fatalf("got %q", got)
	}
}

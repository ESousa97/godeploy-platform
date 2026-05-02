package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWSCheckOrigin(t *testing.T) {
	check := WSCheckOrigin([]string{"https://dashboard.example.com"})
	ctx := context.Background()

	t.Run("no origin", func(t *testing.T) {
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
		if !check(r) {
			t.Fatal("expected allow without Origin")
		}
	})

	t.Run("allowed list", func(t *testing.T) {
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
		r.Header.Set("Origin", "https://dashboard.example.com")
		if !check(r) {
			t.Fatal("expected allow listed origin")
		}
	})

	t.Run("same host", func(t *testing.T) {
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
		r.Host = "app.local:8081"
		r.Header.Set("Origin", "http://app.local:8081")
		if !check(r) {
			t.Fatal("expected allow same host")
		}
	})

	t.Run("reject unknown cross-origin", func(t *testing.T) {
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
		r.Host = "api.local:8081"
		r.Header.Set("Origin", "https://evil.example")
		if check(r) {
			t.Fatal("expected reject unknown origin")
		}
	})
}

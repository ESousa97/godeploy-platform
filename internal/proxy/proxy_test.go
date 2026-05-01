package proxy

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:proxytest?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestProxyRoutesByHostAndHotReload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := openTestSQLite(t)
	p, err := New(Config{
		Addr:         "127.0.0.1:0",
		DB:           db,
		PollInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}
	if err := p.store.EnsureSchema(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}

	// Upstream A
	upA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "A")
	}))
	defer upA.Close()

	// Upstream B
	upB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "B")
	}))
	defer upB.Close()

	// Extract host:port (without scheme)
	targetA := upA.Listener.Addr().String()
	targetB := upB.Listener.Addr().String()

	if err := p.store.UpsertRoute(ctx, "app.local", targetA); err != nil {
		t.Fatalf("upsert A: %v", err)
	}
	p.NotifyReload()
	if err := p.reloadIfChanged(ctx, true); err != nil {
		t.Fatalf("initial load: %v", err)
	}
	go p.reloadLoop(ctx)

	// First request must reach A
	req := httptest.NewRequest("GET", "http://app.local/", nil)
	req.Host = "app.local"
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)
	if rr.Code != 200 || rr.Body.String() != "A" {
		t.Fatalf("expected A 200, got %d body=%q", rr.Code, rr.Body.String())
	}

	// Update route to B, hot reload should pick it up
	if err := p.store.UpsertRoute(ctx, "app.local", targetB); err != nil {
		t.Fatalf("upsert B: %v", err)
	}
	p.NotifyReload()

	deadline := time.Now().Add(2 * time.Second)
	for {
		req2 := httptest.NewRequest("GET", "http://app.local/", nil)
		req2.Host = "app.local"
		rr2 := httptest.NewRecorder()
		p.ServeHTTP(rr2, req2)
		if rr2.Code == 200 && rr2.Body.String() == "B" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected hot reload to B, last=%d body=%q", rr2.Code, rr2.Body.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestProxyHeaderForwarding(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := openTestSQLite(t)
	p, err := New(Config{
		Addr: "127.0.0.1:0",
		DB:   db,
	})
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}
	if err := p.store.EnsureSchema(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}

	var capturedHost, capturedProto, capturedFF string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHost = r.Header.Get("X-Forwarded-Host")
		capturedProto = r.Header.Get("X-Forwarded-Proto")
		capturedFF = r.Header.Get("X-Forwarded-For")
		_, _ = io.WriteString(w, "OK")
	}))
	defer up.Close()

	target := up.Listener.Addr().String()
	if err := p.store.UpsertRoute(ctx, "headers.local", target); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := p.reloadIfChanged(ctx, true); err != nil {
		t.Fatalf("load: %v", err)
	}

	req := httptest.NewRequest("GET", "http://headers.local/foo", nil)
	req.Host = "headers.local"
	req.RemoteAddr = "1.2.3.4:5678"
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if capturedHost != "headers.local" {
		t.Errorf("expected X-Forwarded-Host: headers.local, got %q", capturedHost)
	}
	if capturedProto != "http" {
		t.Errorf("expected X-Forwarded-Proto: http, got %q", capturedProto)
	}
	if capturedFF != "1.2.3.4" {
		t.Errorf("expected X-Forwarded-For: 1.2.3.4, got %q", capturedFF)
	}
}


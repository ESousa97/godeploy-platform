package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"godeploy-platform/internal/middleware"
	"godeploy-platform/internal/pipeline"
	"godeploy-platform/internal/webhook"
)

func TestGetenv(t *testing.T) {
	t.Setenv("GODEPLOY_TEST_K", "  x  ")
	if got := getenv("GODEPLOY_TEST_K", "d"); got != "x" {
		t.Fatalf("getenv = %q", got)
	}
	t.Setenv("GODEPLOY_TEST_K", "")
	if got := getenv("GODEPLOY_TEST_K", "default"); got != "default" {
		t.Fatalf("default = %q", got)
	}
}

func TestGetenvFloat(t *testing.T) {
	t.Setenv("F", "2.5")
	if got := getenvFloat("F", 1); got != 2.5 {
		t.Fatalf("got %v", got)
	}
	t.Setenv("F", "bad")
	if got := getenvFloat("F", 3); got != 3 {
		t.Fatalf("bad -> %v", got)
	}
	t.Setenv("F", "-1")
	if got := getenvFloat("F", 4); got != 4 {
		t.Fatalf("negative -> %v", got)
	}
}

func TestGetenvInt(t *testing.T) {
	t.Setenv("N", "42")
	if got := getenvInt("N", 1); got != 42 {
		t.Fatalf("got %d", got)
	}
	t.Setenv("N", "0")
	if got := getenvInt("N", 7); got != 7 {
		t.Fatalf("zero -> %d", got)
	}
}

func TestSplitCommaList(t *testing.T) {
	got := splitCommaList(" a , ,b, c ")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idx %d: got %q want %q", i, got[i], want[i])
		}
	}
	if len(splitCommaList("")) != 0 {
		t.Fatal("empty should be empty slice")
	}
}

type stubRunner struct {
	called int
	fn     func(context.Context, pipeline.RunRequest) (pipeline.RunResult, error)
}

func (s *stubRunner) Run(ctx context.Context, req pipeline.RunRequest) (pipeline.RunResult, error) {
	s.called++
	if s.fn != nil {
		return s.fn(ctx, req)
	}
	return pipeline.RunResult{}, nil
}

func testServerHandler(t *testing.T, s *server) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	s.registerRoutes(mux, routeDeps{
		docker:       nil,
		wsOrigins:    nil,
		webhookRPS:   100,
		webhookBurst: 100,
	})
	return middleware.SecurityHeaders(mux)
}

func TestHealthz(t *testing.T) {
	s := &server{
		logger: slog.Default(),
		runner: &stubRunner{},
		parser: webhook.Parser{},
	}
	ts := httptest.NewServer(testServerHandler(t, s))
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/healthz", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != "ok" {
		t.Fatalf("body %q", b)
	}
}

func TestWebhookGitHubPing(t *testing.T) {
	stub := &stubRunner{}
	s := &server{
		logger: slog.Default(),
		runner: stub,
		parser: webhook.Parser{},
	}
	ts := httptest.NewServer(testServerHandler(t, s))
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL+"/webhook", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-GitHub-Event", "ping")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != "pong" {
		t.Fatalf("body %q", b)
	}
	if stub.called != 0 {
		t.Fatalf("runner should not run for ping, called=%d", stub.called)
	}
}

func TestWebhookInvalid(t *testing.T) {
	s := &server{
		logger: slog.Default(),
		runner: &stubRunner{},
		parser: webhook.Parser{},
	}
	ts := httptest.NewServer(testServerHandler(t, s))
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL+"/webhook", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	// No GitHub / GitLab headers -> parse error
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", res.StatusCode)
	}
}

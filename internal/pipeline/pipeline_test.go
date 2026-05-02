package pipeline

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"godeploy-platform/internal/detector"

	_ "modernc.org/sqlite"
)

func TestNew_nilDB(t *testing.T) {
	_, err := New(Config{})
	if err == nil || !strings.Contains(err.Error(), "DB") {
		t.Fatalf("New: %v", err)
	}
}

func TestNew_nilDocker(t *testing.T) {
	db, err := sql.Open("sqlite", "file:pipenew?mode=memory")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	_, err = New(Config{DB: db})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "docker") {
		t.Fatalf("New: %v", err)
	}
}

func TestDefaultPortForRuntime(t *testing.T) {
	tests := []struct {
		rt   detector.Runtime
		want int
	}{
		{detector.RuntimeNodeJS, 3000},
		{detector.RuntimePython, 8000},
		{detector.RuntimeStatic, 80},
		{detector.RuntimeGo, 8080},
		{detector.RuntimeDockerfile, 8080},
		{detector.Runtime("unknown"), 8080},
	}
	for _, tt := range tests {
		if got := defaultPortForRuntime(tt.rt); got != tt.want {
			t.Fatalf("%q: got %d want %d", tt.rt, got, tt.want)
		}
	}
}

func TestShortSHA(t *testing.T) {
	if got := shortSHA(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
	if got := shortSHA("abc"); got != "abc" {
		t.Fatalf("short: %q", got)
	}
	if got := shortSHA("1234567890"); got != "1234567" {
		t.Fatalf("trim: %q", got)
	}
}

func TestNormalizeApp(t *testing.T) {
	if got := normalizeApp(""); got != "app" {
		t.Fatalf("empty: %q", got)
	}
	if got := normalizeApp("  My_App "); got != "my-app" {
		t.Fatalf("normalize: %q", got)
	}
}

func TestWaitHTTP200_ok(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	ctx := context.Background()
	if err := waitHTTP200(ctx, ts.URL, 3*time.Second); err != nil {
		t.Fatal(err)
	}
}

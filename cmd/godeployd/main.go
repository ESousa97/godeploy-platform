package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/client"

	"godeploy-platform/internal/observability"
	"godeploy-platform/internal/pipeline"
	"godeploy-platform/internal/webhook"
	_ "modernc.org/sqlite"
)

type server struct {
	logger *log.Logger
	runner *pipeline.Runner
	parser webhook.Parser
}

func main() {
	logger := log.Default()

	addr := getenv("GODEPLOY_ADDR", ":8081")
	dbPath := getenv("GODEPLOY_DB", "godeploy.db")
	networkName := getenv("GODEPLOY_NETWORK", "godeploy")
	secret := strings.TrimSpace(os.Getenv("GODEPLOY_WEBHOOK_SECRET"))

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		logger.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Fatalf("docker client: %v", err)
	}
	defer docker.Close()

	runner, err := pipeline.New(pipeline.Config{
		DB:                 db,
		Docker:             docker,
		NetworkName:        networkName,
		DefaultImagePrefix: getenv("GODEPLOY_IMAGE_PREFIX", "godeploy"),
		HealthTimeout:      30 * time.Second,
		HealthPath:         getenv("GODEPLOY_HEALTH_PATH", "/"),
		Logger:             logger,
	})
	if err != nil {
		logger.Fatalf("pipeline: %v", err)
	}

	s := &server{
		logger: logger,
		runner: runner,
		parser: webhook.Parser{Secret: secret},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /webhook", s.handleWebhook)
	mux.HandleFunc("GET /api/stats", observability.StatsHandler(observability.NewCollector(docker)))
	mux.HandleFunc("GET /api/ws/logs", observability.NewLogsStreamer(docker).Handler())

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Printf("godeployd listening on %s", addr)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			logger.Fatalf("http server: %v", err)
		}
	}
}

func (s *server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()

	ev, err := s.parser.Parse(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ev.Type == "ping" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
		return
	}

	// Domain pode ser enviado pelo client via query (?domain=...) por enquanto.
	// (depois dá pra evoluir para manifest no repo).
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		domain = ev.Domain
	}

	res, runErr := s.runner.Run(ctx, pipeline.RunRequest{
		AppName:    ev.AppName,
		Domain:     domain,
		CloneURL:   ev.CloneURL,
		Ref:        ev.Ref,
		CommitSHA:  ev.CommitSHA,
		HealthPath: strings.TrimSpace(r.URL.Query().Get("health_path")),
	})
	if runErr != nil {
		s.logger.Printf("pipeline failed app=%s provider=%s: %v", ev.AppName, ev.Provider, runErr)
		http.Error(w, runErr.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"provider":         ev.Provider,
		"app":              ev.AppName,
		"runtime":          string(res.Runtime),
		"image_tag":        res.ImageTag,
		"new_container_id": res.NewContainerID,
		"old_container_id": res.OldContainerID,
		"routed_target":    res.RoutedTarget,
	})
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

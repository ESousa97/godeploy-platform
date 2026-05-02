package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/client"

	"godeploy-platform/internal/middleware"
	"godeploy-platform/internal/pipeline"
	"godeploy-platform/internal/platform/iox"
	"godeploy-platform/internal/platform/sqlpool"
	"godeploy-platform/internal/webhook"

	_ "modernc.org/sqlite"
)

// deployRunner is implemented by [*pipeline.Runner] for webhook-triggered deploys.
type deployRunner interface {
	Run(context.Context, pipeline.RunRequest) (pipeline.RunResult, error)
}

type server struct {
	logger *slog.Logger
	runner deployRunner
	parser webhook.Parser
}

const maxWebhookBodyBytes = 1 << 20 // 1 MiB

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)
	os.Exit(run(logger))
}

func run(logger *slog.Logger) int {
	addr := getenv("GODEPLOY_ADDR", ":8081")
	dbPath := getenv("GODEPLOY_DB", "godeploy.db")
	networkName := getenv("GODEPLOY_NETWORK", "godeploy")
	secret := strings.TrimSpace(os.Getenv("GODEPLOY_WEBHOOK_SECRET"))

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		logger.Error("open sqlite", slog.Any("err", err))
		return 1
	}
	sqlpool.ForSQLite(db)
	defer iox.Close(db)

	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Error("docker client", slog.Any("err", err))
		return 1
	}
	defer iox.Close(docker)

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
		logger.Error("pipeline", slog.Any("err", err))
		return 1
	}

	s := &server{
		logger: logger,
		runner: runner,
		parser: webhook.Parser{Secret: secret},
	}

	mux := http.NewServeMux()
	webhookRPS := getenvFloat("GODEPLOY_WEBHOOK_RPS", 5)
	webhookBurst := getenvInt("GODEPLOY_WEBHOOK_BURST", 30)
	wsOrigins := splitCommaList(os.Getenv("GODEPLOY_WS_ALLOWED_ORIGINS"))
	s.registerRoutes(mux, routeDeps{
		docker:       docker,
		wsOrigins:    wsOrigins,
		webhookRPS:   webhookRPS,
		webhookBurst: webhookBurst,
	})

	root := middleware.SecurityHeaders(mux)

	// ReadTimeout applies only until the request (including body) is fully read;
	// handler execution and hijacked WebSocket connections are not bounded by it.
	// WriteTimeout is omitted so long-running webhook handlers can stream the response.
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       3 * time.Minute,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("godeployd listening", slog.String("addr", addr))
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Warn("http shutdown", slog.Any("err", err))
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", slog.Any("err", err))
			return 1
		}
	}
	return 0
}

func (s *server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()

	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)

	ev, err := s.parser.Parse(r)
	if err != nil {
		s.logger.Warn("webhook parse", slog.Any("err", err))
		http.Error(w, "invalid webhook request", http.StatusBadRequest)
		return
	}
	if ev.Type == "ping" {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("pong")); err != nil {
			s.logger.Warn("ping write", slog.Any("err", err))
		}
		return
	}

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
		s.logger.Error("pipeline failed",
			slog.String("app", ev.AppName),
			slog.String("provider", ev.Provider),
			slog.Any("err", runErr),
		)
		http.Error(w, "deploy failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"provider":         ev.Provider,
		"app":              ev.AppName,
		"runtime":          string(res.Runtime),
		"image_tag":        res.ImageTag,
		"new_container_id": res.NewContainerID,
		"old_container_id": res.OldContainerID,
		"routed_target":    res.RoutedTarget,
	}); err != nil {
		s.logger.Warn("webhook json", slog.Any("err", err))
	}
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getenvFloat(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		return def
	}
	return f
}

func getenvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}

func splitCommaList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

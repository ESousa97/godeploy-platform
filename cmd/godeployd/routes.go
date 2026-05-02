package main

import (
	"log/slog"
	"net/http"

	"github.com/docker/docker/client"

	"godeploy-platform/internal/middleware"
	"godeploy-platform/internal/observability"
)

// routeDeps configures HTTP routes that depend on optional services.
// When Docker is nil (tests only), /api/stats and /api/ws/logs are not registered.
type routeDeps struct {
	docker       *client.Client
	wsOrigins    []string
	webhookRPS   float64
	webhookBurst int
}

func (s *server) registerRoutes(mux *http.ServeMux, d routeDeps) {
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("ok")); err != nil {
			logger.Warn("healthz write", slog.Any("err", err))
		}
	})

	mux.Handle("POST /webhook", middleware.WebhookRateLimit(d.webhookRPS, d.webhookBurst, http.HandlerFunc(s.handleWebhook)))

	if d.docker != nil {
		mux.HandleFunc("GET /api/stats", observability.StatsHandler(observability.NewCollector(d.docker), logger))
		mux.HandleFunc("GET /api/ws/logs", observability.NewLogsStreamer(d.docker, d.wsOrigins).Handler())
	}
}

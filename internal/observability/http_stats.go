package observability

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// StatsHandler returns an HTTP handler that writes [StatsResponse] as JSON from c.
func StatsHandler(c *Collector, log *slog.Logger) http.HandlerFunc {
	if log == nil {
		log = slog.Default()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		res, err := c.Collect(ctx)
		if err != nil {
			log.Error("stats collect", slog.Any("err", err))
			http.Error(w, "failed to collect stats", http.StatusInternalServerError)
			return
		}

		payload, encErr := json.Marshal(res)
		if encErr != nil {
			log.Error("stats encode", slog.Any("err", encErr))
			http.Error(w, "failed to encode stats", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, writeErr := w.Write(payload); writeErr != nil {
			log.Warn("stats write", slog.Any("err", writeErr))
		}
	}
}

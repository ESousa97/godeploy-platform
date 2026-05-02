package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type ipLimiter struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// WebhookRateLimit limits POST requests per remote IP (best-effort using RemoteAddr).
func WebhookRateLimit(eventsPerSec float64, burst int, next http.Handler) http.Handler {
	if eventsPerSec <= 0 {
		eventsPerSec = 1
	}
	if burst < 1 {
		burst = 1
	}

	var mu sync.Mutex
	visitors := make(map[string]*ipLimiter)
	lim := rate.Limit(eventsPerSec)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIPKey(r)
		now := time.Now()

		mu.Lock()
		v, ok := visitors[ip]
		if !ok {
			v = &ipLimiter{lim: rate.NewLimiter(lim, burst), lastSeen: now}
			visitors[ip] = v
		}
		v.lastSeen = now
		allow := v.lim.Allow()
		if len(visitors) > 4096 {
			evictOldVisitors(visitors, now.Add(-10*time.Minute))
		}
		mu.Unlock()

		if !allow {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIPKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

func evictOldVisitors(m map[string]*ipLimiter, cutoff time.Time) {
	for k, v := range m {
		if v.lastSeen.Before(cutoff) {
			delete(m, k)
		}
	}
}

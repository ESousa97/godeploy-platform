package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

// WSCheckOrigin returns a gorilla/websocket CheckOrigin function.
// allowed lists extra Origin values (e.g. https://app.example.com). Empty allowed
// still permits requests with no Origin (non-browser) and same-host browser origins.
func WSCheckOrigin(allowed []string) func(r *http.Request) bool {
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		set[strings.ToLower(o)] = struct{}{}
	}

	return func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true
		}
		if _, ok := set[strings.ToLower(origin)]; ok {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		reqHost := strings.TrimSpace(r.Host)
		if reqHost == "" {
			return false
		}
		// Same host (possibly different scheme/port) — typical same-site WS upgrade.
		return strings.EqualFold(strings.TrimSpace(u.Host), reqHost)
	}
}

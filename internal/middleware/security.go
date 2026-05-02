package middleware

import "net/http"

// SecurityHeaders sets baseline HTTP response headers for defense in depth.
// HSTS is omitted: godeployd is commonly run plain HTTP behind a TLS terminator
// (enable HSTS at the edge when TLS terminates there).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

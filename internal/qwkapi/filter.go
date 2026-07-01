package qwkapi

import (
	"net"
	"net/http"
	"strings"
)

// requireClient rejects requests that lack the X-V3-Client header or that look
// like they came from a web browser. It returns 404 (not 403) so the endpoint's
// existence is not confirmed. This filters casual/browser traffic; it is NOT an
// authentication control.
func requireClient(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("X-V3-Client")) == "" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Sec-Fetch-Mode") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
			http.NotFound(w, r)
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

// clientIP extracts the remote IP (host part) for rate-limit keying.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

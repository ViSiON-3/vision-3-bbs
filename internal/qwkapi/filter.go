package qwkapi

import (
	"log/slog"
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
		if reason := clientRejectReason(r); reason != "" {
			logProbe(r, reason)
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

// clientRejectReason names why the client filter turned a request away, or ""
// when the request passes.
func clientRejectReason(r *http.Request) string {
	if strings.TrimSpace(r.Header.Get("X-V3-Client")) == "" {
		return "noClientHeader"
	}
	if r.Header.Get("Sec-Fetch-Mode") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
		return "browser"
	}
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		return "htmlAccept"
	}
	return ""
}

// logProbe records a request rejected before it reached a handler. A public
// port collects constant scanner traffic, so this sits at DEBUG: a sysop turns
// the log level down to investigate rather than having every probe flood the
// normal log.
func logProbe(r *http.Request, reason string) {
	slog.Debug("qwk api rejected",
		"remote", clientIP(r), "method", r.Method, "path", r.URL.Path,
		"reason", reason, "agent", r.UserAgent())
}

// clientIP extracts the remote IP (host part) for rate-limit keying.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

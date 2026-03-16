package hub

import (
	"net/http"
	"strings"
)

// Mux returns the HTTP handler for the hub. Used by the standalone hub binary
// and integration tests.
func (h *Hub) Mux() http.Handler {
	return h.newMux()
}

// newMux creates the HTTP router for the hub.
func (h *Hub) newMux() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimRight(r.URL.Path, "/")

		// Public endpoints (no auth).
		switch {
		case path == "/v3net/v1/networks" && r.Method == http.MethodGet:
			h.handleNetworks(w, r)
			return
		case path == "/v3net/v1/subscribe" && r.Method == http.MethodPost:
			h.handleSubscribe(w, r)
			return
		case strings.HasSuffix(path, "/info") && r.Method == http.MethodGet:
			h.handleNetworkInfo(w, r)
			return
		}

		// All other /v3net/v1/{network}/* routes require auth.
		network := extractNetwork(r.URL.Path)
		if network == "" {
			http.NotFound(w, r)
			return
		}

		h.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(path, "/messages") && r.Method == http.MethodGet:
				h.handleGetMessages(w, r)
			case strings.HasSuffix(path, "/messages") && r.Method == http.MethodPost:
				h.handlePostMessage(w, r)
			case strings.HasSuffix(path, "/events") && r.Method == http.MethodGet:
				h.handleEvents(w, r)
			case strings.HasSuffix(path, "/chat") && r.Method == http.MethodPost:
				h.handleChat(w, r)
			case strings.HasSuffix(path, "/presence") && r.Method == http.MethodPost:
				h.handlePresence(w, r)
			default:
				http.NotFound(w, r)
			}
		})).ServeHTTP(w, r)
	})
}

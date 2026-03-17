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
		case strings.HasSuffix(path, "/nal") && r.Method == http.MethodGet:
			h.handleGetNAL(w, r)
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
			// Core endpoints.
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

			// NAL endpoint (POST requires coordinator auth).
			case strings.HasSuffix(path, "/nal") && r.Method == http.MethodPost:
				h.handlePostNAL(w, r)

			// Area proposals.
			case strings.HasSuffix(path, "/areas/propose") && r.Method == http.MethodPost:
				h.handlePropose(w, r)
			case strings.HasSuffix(path, "/areas/proposals") && r.Method == http.MethodGet:
				h.handleListProposals(w, r)
			case strings.HasSuffix(path, "/approve") && strings.Contains(path, "/proposals/") && r.Method == http.MethodPost:
				h.handleApproveProposal(w, r)
			case strings.HasSuffix(path, "/reject") && strings.Contains(path, "/proposals/") && r.Method == http.MethodPost:
				h.handleRejectProposal(w, r)

			// Area access management.
			case h.matchAreaAccessPath(path, "/access") && r.Method == http.MethodGet:
				h.handleGetAccess(w, r)
			case h.matchAreaAccessPath(path, "/access/mode") && r.Method == http.MethodPost:
				h.handleSetAccessMode(w, r)
			case h.matchAreaAccessPath(path, "/access/requests") && r.Method == http.MethodGet:
				h.handleListAccessRequests(w, r)
			case h.matchAreaAccessPath(path, "/access/approve") && r.Method == http.MethodPost:
				h.handleApproveAccess(w, r)
			case h.matchAreaAccessPath(path, "/access/deny") && r.Method == http.MethodPost:
				h.handleDenyAccess(w, r)
			case h.matchAreaAccessPath(path, "/access/remove") && r.Method == http.MethodPost:
				h.handleRemoveFromDenyList(w, r)

			// Coordinator transfer.
			case strings.HasSuffix(path, "/coordinator/transfer") && r.Method == http.MethodPost:
				h.handleCoordTransfer(w, r)
			case strings.HasSuffix(path, "/coordinator/accept") && r.Method == http.MethodPost:
				h.handleCoordAccept(w, r)

			default:
				http.NotFound(w, r)
			}
		})).ServeHTTP(w, r)
	})
}

// matchAreaAccessPath checks if a path matches /v3net/v1/{network}/areas/{tag}/{suffix}.
func (h *Hub) matchAreaAccessPath(path, suffix string) bool {
	return strings.Contains(path, "/areas/") && strings.HasSuffix(path, suffix)
}

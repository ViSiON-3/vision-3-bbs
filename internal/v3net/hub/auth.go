package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

const (
	headerNodeID    = "X-V3Net-Node-ID"
	headerSignature = "X-V3Net-Signature"
	maxClockSkew    = 5 * time.Minute
)

// authMiddleware validates request authentication for protected endpoints.
// It extracts the node ID and network from the request, verifies the signature,
// and checks clock skew.
func (h *Hub) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.Header.Get(headerNodeID)
		sig := r.Header.Get(headerSignature)
		dateStr := r.Header.Get("Date")

		if nodeID == "" || sig == "" || dateStr == "" {
			http.Error(w, `{"error":"missing auth headers"}`, http.StatusUnauthorized)
			return
		}

		// Check clock skew.
		reqTime, err := http.ParseTime(dateStr)
		if err != nil {
			http.Error(w, `{"error":"invalid Date header"}`, http.StatusUnauthorized)
			return
		}
		if time.Since(reqTime).Abs() > maxClockSkew {
			http.Error(w, `{"error":"request time outside acceptable range"}`, http.StatusUnauthorized)
			return
		}

		// Extract network from path to look up subscriber.
		network := extractNetwork(r.URL.Path)
		if network == "" {
			http.Error(w, `{"error":"cannot determine network from path"}`, http.StatusBadRequest)
			return
		}

		pubKey := h.subscribers.GetPubKey(nodeID, network)
		if pubKey == nil {
			http.Error(w, `{"error":"unknown or inactive node"}`, http.StatusUnauthorized)
			return
		}

		// Limit request body to 64KB to prevent resource exhaustion.
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)

		// Compute body hash.
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, `{"error":"request body too large or unreadable"}`, http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))

		bodyHash := sha256.Sum256(bodyBytes)
		bodySHA := hex.EncodeToString(bodyHash[:])

		// Build canonical path with query string.
		canonPath := r.URL.Path
		if r.URL.RawQuery != "" {
			canonPath += "?" + r.URL.RawQuery
		}

		if !keystore.Verify(pubKey, r.Method, canonPath, dateStr, bodySHA, sig) {
			http.Error(w, `{"error":"invalid signature"}`, http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// extractNetwork pulls the network name from a V3Net API path.
// Expected paths: /v3net/v1/{network}/messages, /v3net/v1/{network}/events, etc.
func extractNetwork(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// v3net/v1/{network}/...
	if len(parts) >= 3 && parts[0] == "v3net" && parts[1] == "v1" {
		return parts[2]
	}
	return ""
}

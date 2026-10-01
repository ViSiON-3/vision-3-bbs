package hub

import (
	"bytes"
	"crypto/ed25519"
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

		if msg := checkRequestDate(dateStr); msg != "" {
			http.Error(w, msg, http.StatusUnauthorized)
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
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		if !signatureValid(r, bodyBytes, pubKey) {
			http.Error(w, `{"error":"invalid signature"}`, http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// checkRequestDate validates a request's Date header against maxClockSkew.
// It returns a JSON error body, or "" if the date is acceptable.
func checkRequestDate(dateStr string) string {
	reqTime, err := http.ParseTime(dateStr)
	if err != nil {
		return `{"error":"invalid Date header"}`
	}
	if time.Since(reqTime).Abs() > maxClockSkew {
		return `{"error":"request time outside acceptable range"}`
	}
	return ""
}

// signatureValid reports whether the request's signature header is a valid
// signature by pubKey over its method, path (with query), Date header and
// the SHA-256 of body.
func signatureValid(r *http.Request, body []byte, pubKey ed25519.PublicKey) bool {
	bodyHash := sha256.Sum256(body)
	bodySHA := hex.EncodeToString(bodyHash[:])

	// Build canonical path with query string.
	canonPath := r.URL.Path
	if r.URL.RawQuery != "" {
		canonPath += "?" + r.URL.RawQuery
	}

	return keystore.Verify(pubKey, r.Method, canonPath, r.Header.Get("Date"), bodySHA, r.Header.Get(headerSignature))
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

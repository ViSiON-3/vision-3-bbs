package qwkapi

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Authenticator verifies BBS credentials (satisfied by *user.UserMgr).
type Authenticator interface {
	Authenticate(handle, password string) (*user.User, bool)
}

type loginRequest struct {
	Handle   string `json:"handle"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

type replyResponse struct {
	Posted    int  `json:"posted"`
	Skipped   int  `json:"skipped"`
	Duplicate int  `json:"duplicate"`
	WrongBBS  bool `json:"wrongBBS"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

// writeLockedOut answers a login refused by a lockout with 429, telling the
// client when to try again.
func writeLockedOut(w http.ResponseWriter, until time.Time, message string) {
	secs := int(math.Ceil(time.Until(until).Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeError(w, http.StatusTooManyRequests, "locked_out", message)
}

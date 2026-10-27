package qwkapi

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Authenticator verifies BBS credentials and re-reads accounts for live
// sessions (satisfied by *user.UserMgr).
type Authenticator interface {
	Authenticate(handle, password string) (*user.User, bool)
	GetUserByID(id int) (*user.User, bool)
	// SyncFromDisk folds in edits made to users.json outside the BBS (the
	// standalone ./ue editor), at most once a second. The API calls it before
	// every account read, since with no terminal caller online nothing else
	// would, and a ban or password change made in ./ue would go unseen.
	SyncFromDisk()
}

// Reasons a DenyLogin hook gives for refusing an account whose password is
// correct. Each is also the error code the client receives.
const (
	// DenyIntroPending: the account still owes the sysop its new-user
	// introduction, which only the terminal can collect.
	DenyIntroPending = "intro_pending"
	// DenyLogonLevel: the account's access level is below logonLevel (this is
	// also what a banned account, at level 0, hits).
	DenyLogonLevel = "logon_level"
)

var denyMessages = map[string]string{
	DenyIntroPending: "finish new-user signup on the BBS first",
	DenyLogonLevel:   "account not permitted to log in",
}

// DeviceStore keeps the devices paired with each account (satisfied by
// *user.UserMgr).
type DeviceStore interface {
	AddAPIDevice(userID int, d user.APIDevice, max int) error
	RemoveAPIDevice(userID int, deviceID string) (bool, error)
	TouchAPIDevice(userID int, deviceID string, at time.Time) error
	FindAPIDevice(deviceID string) (*user.User, user.APIDevice, bool)
	// BeginSession records a device login as a call, as Authenticate does
	// for a password login.
	BeginSession(handle string) (*user.User, bool)
}

type loginRequest struct {
	Handle   string `json:"handle"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
	Scope     string `json:"scope"`
}

type pairRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
}

type pairResponse struct {
	DeviceID string `json:"deviceId"`
	BoardID  string `json:"boardId"`
}

type challengeRequest struct {
	DeviceID string `json:"deviceId"`
}

type challengeResponse struct {
	Challenge string `json:"challenge"`
	ExpiresAt string `json:"expiresAt"`
}

type deviceLoginRequest struct {
	DeviceID  string `json:"deviceId"`
	Challenge string `json:"challenge"`
	Signature string `json:"signature"` // base64
}

type deviceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
	LastUsed  string `json:"lastUsed,omitempty"`
	Current   bool   `json:"current"`
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

// writeDenied answers for an account a DenyLogin hook refused, with the reason
// as the error code.
func writeDenied(w http.ResponseWriter, reason string) {
	msg, known := denyMessages[reason]
	if !known {
		msg = denyMessages[DenyLogonLevel]
	}
	writeError(w, http.StatusForbidden, reason, msg)
}

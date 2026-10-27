package qwkapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/logging"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// maxDeviceIDLen bounds a client-supplied device ID. Real IDs are 32 hex
// characters; anything much longer is not one of ours.
const maxDeviceIDLen = 64

// handleDevices serves the device list (GET, either scope) and pairing (POST,
// pair tokens only: registering a key must be backed by the password, or a
// stolen device could enroll others and outlive its own unpairing).
func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.requireAnySession(s.handleDeviceList)(w, r)
	case http.MethodPost:
		s.requireSession(scopePair, s.handlePair)(w, r)
	default:
		logProbe(r, "badMethod")
		writeError(w, http.StatusMethodNotAllowed, "method", "GET or POST required")
	}
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	var req pairRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	_, canonical, err := parseDeviceKey(req.PublicKey)
	if err != nil {
		slog.Info("qwk api pair", "handle", u.Handle, "remote", clientIP(r), "outcome", "badKey", "error", err)
		writeError(w, http.StatusBadRequest, "bad_key", err.Error())
		return
	}
	id, err := randomID(16)
	if err != nil {
		slog.Error("qwk api pair: device ID failed", "handle", u.Handle, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not pair device")
		return
	}
	dev := user.APIDevice{ID: id, Name: cleanDeviceName(req.Name), PublicKey: canonical, CreatedAt: time.Now().UTC()}
	switch err := s.deps.Devices.AddAPIDevice(u.ID, dev, maxDevicesPerUser); {
	case errors.Is(err, user.ErrTooManyDevices):
		slog.Info("qwk api pair", "handle", u.Handle, "remote", clientIP(r), "outcome", "tooMany")
		writeError(w, http.StatusConflict, "too_many_devices", "unpair a device first")
		return
	case errors.Is(err, user.ErrDeviceKeyExists):
		slog.Info("qwk api pair", "handle", u.Handle, "remote", clientIP(r), "outcome", "keyExists")
		writeError(w, http.StatusConflict, "key_exists", "this key is already paired")
		return
	case err != nil:
		slog.Error("qwk api pair: save failed", "handle", u.Handle, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not pair device")
		return
	}
	logging.Security("qwk api device paired", "handle", u.Handle, "device", dev.ID, "name", dev.Name, "remote", clientIP(r))
	writeJSON(w, http.StatusCreated, pairResponse{DeviceID: dev.ID, BoardID: s.deps.BoardID})
}

func (s *Server) handleDeviceList(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	out := []deviceInfo{}
	for _, d := range sess.user.ValidAPIDevices() {
		info := deviceInfo{ID: d.ID, Name: d.Name, CreatedAt: d.CreatedAt.UTC().Format(time.RFC3339), Current: d.ID == sess.deviceID}
		if !d.LastUsed.IsZero() {
			info.LastUsed = d.LastUsed.UTC().Format(time.RFC3339)
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDeviceDelete unpairs a device. Either scope may do it, so a user on a
// new phone can drop a lost one after a password login. Unpairing the device
// the request came from ends that session on its next request.
func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		logProbe(r, "badMethod")
		writeError(w, http.StatusMethodNotAllowed, "method", "DELETE required")
		return
	}
	u := userFromContext(r.Context())
	id := strings.TrimPrefix(r.URL.Path, "/api/qwk/devices/")
	if id == "" || strings.Contains(id, "/") || len(id) > maxDeviceIDLen {
		writeError(w, http.StatusNotFound, "not_found", "no such device")
		return
	}
	removed, err := s.deps.Devices.RemoveAPIDevice(u.ID, id)
	if err != nil {
		slog.Error("qwk api unpair: save failed", "handle", u.Handle, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not unpair device")
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, "not_found", "no such device")
		return
	}
	logging.Security("qwk api device unpaired", "handle", u.Handle, "device", id, "remote", clientIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleChallenge issues a single-use challenge for a device to sign.
func (s *Server) handleChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		logProbe(r, "badMethod")
		writeError(w, http.StatusMethodNotAllowed, "method", "POST required")
		return
	}
	ip := clientIP(r)
	if !s.challLimit.allow(ip) {
		slog.Warn("qwk api challenge", "remote", ip, "outcome", "rateLimited")
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many challenge requests")
		return
	}
	var req challengeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || req.DeviceID == "" || len(req.DeviceID) > maxDeviceIDLen {
		writeError(w, http.StatusBadRequest, "bad_request", "deviceId required")
		return
	}
	ch, exp, err := s.challenges.issue(req.DeviceID)
	if errors.Is(err, errChallengeStoreFull) {
		slog.Warn("qwk api challenge", "remote", ip, "outcome", "storeFull")
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusServiceUnavailable, "busy", "try again shortly")
		return
	}
	if err != nil {
		slog.Error("qwk api challenge: issue failed", "remote", ip, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not issue challenge")
		return
	}
	writeJSON(w, http.StatusOK, challengeResponse{Challenge: ch, ExpiresAt: exp.UTC().Format(time.RFC3339)})
}

// handleDeviceLogin exchanges a signed challenge for a device token. A bad
// signature or unknown device counts as a failed login toward the BBS's
// per-IP lockout, like a wrong password.
func (s *Server) handleDeviceLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		logProbe(r, "badMethod")
		writeError(w, http.StatusMethodNotAllowed, "method", "POST required")
		return
	}
	ip := clientIP(r)
	if s.deps.Guard != nil {
		if locked, until, _ := s.deps.Guard.IsIPLockedOut(ip); locked {
			logging.Security("qwk api device login from locked IP", "remote", ip, "locked_until", until.Format(time.RFC3339))
			writeLockedOut(w, until, "too many failed logins from this address")
			return
		}
	}
	if !s.loginLimit.allow(ip) {
		slog.Warn("qwk api device login", "remote", ip, "outcome", "rateLimited")
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts")
		return
	}
	var req deviceLoginRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		slog.Info("qwk api device login", "remote", ip, "outcome", "badRequest", "error", err)
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	// An expired or reused challenge is what a slow network looks like as
	// much as a replay, so it is reported but not counted as a failed login.
	if !s.challenges.consume(req.Challenge, req.DeviceID) {
		slog.Info("qwk api device login", "device", req.DeviceID, "remote", ip, "outcome", "badChallenge")
		writeError(w, http.StatusUnauthorized, "challenge_invalid", "challenge expired or already used; request a new one")
		return
	}
	fail := func(outcome string) {
		slog.Info("qwk api device login", "device", req.DeviceID, "remote", ip, "outcome", outcome)
		if s.deps.Guard != nil {
			s.deps.Guard.RecordFailedLoginAttempt(ip)
		}
		writeError(w, http.StatusUnauthorized, "unauthorized", "device login failed")
	}
	s.deps.Users.SyncFromDisk()
	u, dev, found := s.deps.Devices.FindAPIDevice(req.DeviceID)
	if !found || u.DeletedUser {
		fail("unknownDevice")
		return
	}
	pub, _, err := parseDeviceKey(dev.PublicKey)
	if err != nil {
		slog.Error("qwk api device login: stored key unreadable", "handle", u.Handle, "device", dev.ID, "error", err)
		fail("badKey")
		return
	}
	sig, err := base64.StdEncoding.DecodeString(req.Signature)
	if err != nil || !verifyDeviceSignature(pub, deviceLoginMessage(s.deps.BoardID, dev.ID, req.Challenge), sig) {
		fail("badSignature")
		return
	}
	if reason := s.deps.DenyLogin(u); reason != "" {
		slog.Info("qwk api device login", "handle", u.Handle, "device", dev.ID, "remote", ip, "outcome", "denied", "reason", reason)
		writeDenied(w, reason)
		return
	}
	snap, ok := s.deps.Devices.BeginSession(u.Handle)
	if !ok {
		fail("unknownDevice")
		return
	}
	if s.deps.Guard != nil {
		s.deps.Guard.ClearFailedLoginAttempts(ip)
	}
	tok, exp, err := s.tokens.Issue(snap, scopeDevice, dev.ID)
	if err != nil {
		slog.Error("qwk api device login: token issue failed", "handle", u.Handle, "remote", ip, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not issue token")
		return
	}
	if err := s.deps.Devices.TouchAPIDevice(snap.ID, dev.ID, time.Now().UTC()); err != nil {
		slog.Warn("qwk api device login: could not record last use", "handle", u.Handle, "device", dev.ID, "error", err)
	}
	slog.Info("qwk api device login", "handle", snap.Handle, "device", dev.ID, "remote", ip, "outcome", "success")
	writeJSON(w, http.StatusOK, loginResponse{Token: tok, ExpiresAt: exp.UTC().Format(time.RFC3339), Scope: string(scopeDevice)})
}

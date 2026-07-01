package qwkapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/qwkservice"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method", "POST required")
		return
	}
	ip := clientIP(r)
	if !s.loginLimit.allow(ip) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts")
		return
	}
	var req loginRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	u, ok := s.deps.Users.Authenticate(req.Handle, req.Password)
	if !ok {
		slog.Info("qwk api login", "handle", req.Handle, "remote", ip, "outcome", "fail")
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}
	tok, exp, err := s.tokens.Issue(u)
	if err != nil {
		slog.Error("qwk api login: token issue failed", "handle", u.Handle, "remote", ip, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not issue token")
		return
	}
	slog.Info("qwk api login", "handle", u.Handle, "remote", ip, "outcome", "success")
	writeJSON(w, http.StatusOK, loginResponse{Token: tok, ExpiresAt: exp.UTC().Format("2006-01-02T15:04:05Z07:00")})
}

func (s *Server) handlePacket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method", "GET required")
		return
	}
	u := userFromContext(r.Context())
	if !s.packetLimit.allow(u.Handle) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "slow down")
		return
	}
	res, err := s.deps.Service.BuildPacket(qwkservice.ExportOptions{
		Handle:     u.Handle,
		TaggedTags: u.TaggedMessageAreaTags,
	})
	if err != nil {
		slog.Error("qwk api build packet", "handle", u.Handle, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "failed to build packet")
		return
	}
	if res.MessageCount == 0 {
		slog.Info("qwk api packet", "handle", u.Handle, "remote", clientIP(r), "messages", 0)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Commit lastread only after confirming a non-empty, error-free export, so
	// the newscan pointers never advance on an empty or failed download (mirror
	// the terminal path).
	s.deps.Service.CommitExport(u.Handle, res)
	slog.Info("qwk api packet", "handle", u.Handle, "remote", clientIP(r), "messages", res.MessageCount)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-QWK-Messages", strconv.Itoa(res.MessageCount))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(res.Packet)
}

func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method", "POST required")
		return
	}
	u := userFromContext(r.Context())
	if !s.packetLimit.allow(u.Handle) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "slow down")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxREPBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "read error")
		return
	}
	if len(data) > maxREPBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "packet exceeds 16 MiB")
		return
	}
	res, err := s.deps.Service.ImportREP(data, qwkservice.ImportOptions{
		Handle:    u.Handle,
		Signature: u.AutoSignature,
		Authorize: s.deps.AuthorizeFor(u),
	})
	if err != nil {
		if errors.Is(err, qwkservice.ErrWrongBBS) {
			slog.Info("qwk api reply", "handle", u.Handle, "remote", clientIP(r), "outcome", "wrongBBS")
			writeJSON(w, http.StatusOK, replyResponse{WrongBBS: true})
			return
		}
		slog.Error("qwk api import rep", "handle", u.Handle, "error", err)
		writeError(w, http.StatusBadRequest, "bad_packet", "could not process packet")
		return
	}
	slog.Info("qwk api reply", "handle", u.Handle, "remote", clientIP(r),
		"posted", res.Posted, "skipped", res.Skipped, "duplicate", res.Duplicate)
	writeJSON(w, http.StatusOK, replyResponse{Posted: res.Posted, Skipped: res.Skipped, Duplicate: res.Duplicate})
}

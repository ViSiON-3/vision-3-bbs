// Package qwkapi is the HTTPS API that lets an offline mail reader fetch QWK
// packets and upload REP replies without a terminal session. A client POSTs
// BBS credentials to /api/qwk/login for a short-lived bearer token, then GETs
// /api/qwk/packet and POSTs to /api/qwk/reply, ending with an optional POST to
// /api/qwk/logout; packet building and reply import are delegated to
// qwkservice. Server, built by NewServer, adds rate
// limiting, a client-header filter and a sysop-supplied or self-signed TLS
// certificate.
package qwkapi

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwkservice"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// PacketService is the subset of *qwkservice.Service the API needs.
type PacketService interface {
	BuildPacket(opts qwkservice.ExportOptions) (*qwkservice.ExportResult, error)
	CommitExport(handle string, res *qwkservice.ExportResult)
	ImportREP(data []byte, opts qwkservice.ImportOptions) (*qwkservice.ImportResult, error)
}

// Deps are the collaborators the API server needs.
type Deps struct {
	// TwitMatcherFor resolves the current list and board exemptions for each
	// download. Required: the API has no access to the board's Twit List
	// settings, so it can't build a correct matcher on its own.
	TwitMatcherFor func(*user.User) *user.TwitMatcher
	Config         config.QWKAPIConfig
	ConfigDir      string // where auto TLS certs live
	Users          Authenticator
	Service        PacketService
	AuthorizeFor   func(u *user.User) func(area *message.MessageArea) bool
	// DenyLogin applies the checks the terminal login makes once a password
	// verifies, returning a DenyIntroPending/DenyLogonLevel reason or "" to
	// admit. It runs at login and again on every authenticated request.
	// Required, so no API login skips them.
	DenyLogin func(u *user.User) string
	// Guard, when non-nil, applies the BBS's IP blocklist to every request and
	// its per-IP failed-login lockout to logins.
	Guard LoginGuard
}

// Server is the QWK packet transport API.
type Server struct {
	deps        Deps
	tokens      *tokenStore
	loginLimit  *limiter
	packetLimit *limiter
	handleLocks *handleLockouts
	cert        tls.Certificate
	fingerprint string
	httpSrv     *http.Server
	done        chan struct{}
	closeOnce   sync.Once
}

// maxREPBytes is the upload cap for REP packets (16 MiB).
const maxREPBytes = 16 << 20

// NewServer builds the server and resolves its TLS certificate.
func NewServer(deps Deps) (*Server, error) {
	if deps.TwitMatcherFor == nil {
		return nil, errors.New("qwk api: TwitMatcherFor is required")
	}
	if deps.DenyLogin == nil {
		return nil, errors.New("qwk api: DenyLogin is required")
	}
	cert, fp, err := loadOrCreateCert(deps.Config, deps.ConfigDir)
	if err != nil {
		return nil, err
	}
	return &Server{
		deps:        deps,
		tokens:      newTokenStore(deps.Config.TokenTTL()),
		loginLimit:  newLimiter(5, time.Minute),
		packetLimit: newLimiter(30, time.Minute),
		handleLocks: newHandleLockouts(handleMaxFailures, handleLockout),
		cert:        cert,
		fingerprint: fp,
		done:        make(chan struct{}),
	}, nil
}

// Handler builds the routed, middleware-wrapped handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/qwk/login", s.rejectBlocked(requireClient(s.handleLogin)))
	mux.HandleFunc("/api/qwk/logout", s.rejectBlocked(requireClient(s.handleLogout)))
	mux.HandleFunc("/api/qwk/packet", s.rejectBlocked(requireClient(s.requireSession(s.handlePacket))))
	mux.HandleFunc("/api/qwk/reply", s.rejectBlocked(requireClient(s.requireSession(s.handleReply))))
	// Catch-all so requests to unknown paths are visible too; without it the
	// mux's own 404 would leave scanner traffic entirely unrecorded.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		logProbe(r, "unknownPath")
		http.NotFound(w, r)
	})
	return logRedirects(mux)
}

// logRedirects records the redirects ServeMux issues for unclean paths
// ("/api//qwk/login", "/a/../b"). The mux answers those itself, before any
// registered handler runs — including the catch-all above — so without this
// wrapper they would be the one class of request that reaches the port and
// leaves no trace at all.
func logRedirects(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sr := &statusRecorder{ResponseWriter: w}
		h.ServeHTTP(sr, r)
		switch sr.status {
		case http.StatusMovedPermanently, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			logProbe(r, "pathRedirect")
		}
	})
}

// statusRecorder remembers the status code written through it. A handler that
// never calls WriteHeader leaves status at 0, which no case above matches.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Start serves HTTPS until Shutdown is called; blocking.
func (s *Server) Start() error {
	s.httpSrv = &http.Server{
		Addr:      s.deps.Config.ListenAddr(),
		Handler:   s.Handler(),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{s.cert}},
		// Route net/http's own connection-level errors (TLS handshake failures,
		// malformed requests) through slog instead of the stdlib log bridge, so
		// they land in the rolling log as structured records like everything else.
		ErrorLog: log.New(serverErrorWriter{}, "", 0),
	}
	slog.Info("QWK API listening", "addr", s.deps.Config.ListenAddr(), "fingerprint", s.fingerprint)
	go s.sweepLoop()
	// Stop the sweeper whenever Start returns — including an immediate listen
	// failure (e.g. bind error) where Shutdown is never called.
	defer s.closeOnce.Do(func() { close(s.done) })
	if err := s.httpSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("qwk api serve: %w", err)
	}
	return nil
}

// sweepLoop periodically prunes expired tokens, elapsed rate-limit windows and
// stale handle lockouts until the server is shut down, bounding the in-memory
// maps.
func (s *Server) sweepLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.tokens.sweep()
			s.loginLimit.sweep()
			s.packetLimit.sweep()
			s.handleLocks.sweep()
		}
	}
}

// Shutdown gracefully stops the server and its sweeper.
func (s *Server) Shutdown(ctx context.Context) error {
	s.closeOnce.Do(func() { close(s.done) })
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}

// serverErrorWriter adapts http.Server's stdlib logger to slog. Each line net/http
// writes (e.g. "http: TLS handshake error from 1.2.3.4:5678: ...") becomes one
// WARN record: these are connection-level problems, not BBS-level failures.
type serverErrorWriter struct{}

func (serverErrorWriter) Write(p []byte) (int, error) {
	slog.Warn("qwk api server", "error", strings.TrimRight(string(p), "\r\n"))
	return len(p), nil
}

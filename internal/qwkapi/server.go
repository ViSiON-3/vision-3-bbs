package qwkapi

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	Config       config.QWKAPIConfig
	ConfigDir    string // where auto TLS certs live
	Users        Authenticator
	Service      PacketService
	AuthorizeFor func(u *user.User) func(area *message.MessageArea) bool
}

// Server is the QWK packet transport API.
type Server struct {
	deps        Deps
	tokens      *tokenStore
	loginLimit  *limiter
	packetLimit *limiter
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
	cert, fp, err := loadOrCreateCert(deps.Config, deps.ConfigDir)
	if err != nil {
		return nil, err
	}
	return &Server{
		deps:        deps,
		tokens:      newTokenStore(deps.Config.TokenTTL()),
		loginLimit:  newLimiter(5, time.Minute),
		packetLimit: newLimiter(30, time.Minute),
		cert:        cert,
		fingerprint: fp,
		done:        make(chan struct{}),
	}, nil
}

// Handler builds the routed, middleware-wrapped handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/qwk/login", requireClient(s.handleLogin))
	mux.HandleFunc("/api/qwk/packet", requireClient(s.tokens.requireBearer(s.handlePacket)))
	mux.HandleFunc("/api/qwk/reply", requireClient(s.tokens.requireBearer(s.handleReply)))
	return mux
}

// Start serves HTTPS until Shutdown is called; blocking.
func (s *Server) Start() error {
	s.httpSrv = &http.Server{
		Addr:      s.deps.Config.ListenAddr(),
		Handler:   s.Handler(),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{s.cert}},
	}
	slog.Info("QWK API listening", "addr", s.deps.Config.ListenAddr(), "fingerprint", s.fingerprint)
	go s.sweepLoop()
	if err := s.httpSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("qwk api serve: %w", err)
	}
	return nil
}

// sweepLoop periodically prunes expired tokens and elapsed rate-limit windows
// until the server is shut down, bounding the in-memory maps.
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

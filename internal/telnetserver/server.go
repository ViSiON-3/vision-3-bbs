package telnetserver

import (
	"fmt"
	"log/slog"
	"net"
	"sync"
)

// SessionHandler is called when a new telnet session is established.
type SessionHandler func(*TelnetSessionAdapter)

// Config holds telnet server configuration.
type Config struct {
	Port           int
	Host           string
	SessionHandler SessionHandler
}

// Server is a telnet server that listens for TCP connections
// and wraps them with telnet protocol handling.
type Server struct {
	listener net.Listener
	config   Config
	mu       sync.Mutex
}

// NewServer creates a new telnet server instance.
func NewServer(cfg Config) (*Server, error) {
	if cfg.SessionHandler == nil {
		return nil, fmt.Errorf("session handler is required")
	}
	if cfg.Port <= 0 {
		return nil, fmt.Errorf("invalid port: %d", cfg.Port)
	}
	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}

	return &Server{config: cfg}, nil
}

// ListenAndServe starts listening for telnet connections and blocks.
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()

	slog.Info("telnet server listening", "addr", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			// Check if server was closed
			s.mu.Lock()
			closed := s.listener == nil
			s.mu.Unlock()
			if closed {
				return nil // Clean shutdown
			}
			slog.Error("telnet accept error", "error", err)
			continue
		}

		go s.handleConnection(conn)
	}
}

// handleConnection processes a new telnet connection.
func (s *Server) handleConnection(conn net.Conn) {
	remoteAddr := conn.RemoteAddr().String()
	slog.Info("telnet connection", "remote", remoteAddr)

	defer func() {
		if r := recover(); r != nil {
			slog.Error("telnet panic handling connection", "remote", remoteAddr, "panic", r)
		}
		_ = conn.Close() // best-effort close on teardown
		slog.Info("telnet connection closed", "remote", remoteAddr)
	}()

	// Create telnet-aware connection wrapper
	tc := NewTelnetConn(conn)

	// Negotiate telnet options (ECHO, SGA, NAWS, etc.)
	if err := tc.Negotiate(); err != nil {
		slog.Error("telnet negotiation failed", "remote", remoteAddr, "error", err)
		return
	}

	// Detect actual usable terminal size via ANSI CPR (primary), NAWS (fallback), defaults
	// CPR detects status bars (e.g., SyncTerm reports 25 via NAWS but only 24 rows usable)
	w, h, method := tc.DetectTerminalSize()
	slog.Info("telnet session", "remote", remoteAddr, "width", w, "height", h, "method", method)

	// Create session adapter that implements ssh.Session
	adapter := NewTelnetSessionAdapter(tc)

	// Call the session handler (same as SSH sessions use)
	s.config.SessionHandler(adapter)
}

// Close shuts down the telnet server.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		err := s.listener.Close()
		s.listener = nil
		return err
	}
	return nil
}

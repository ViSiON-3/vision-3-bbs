// Package sshserver provides a pure-Go SSH server for Vision/3 BBS.
// It wraps gliderlabs/ssh (which itself wraps golang.org/x/crypto/ssh)
// and adds BBS-specific features like legacy algorithm support for
// retro terminal clients (SyncTERM, NetRunner) and read-interruptible
// sessions for clean door program I/O cancellation.
package sshserver

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// ErrReadInterrupted is returned by BBSSession.Read when a read interrupt fires.
var ErrReadInterrupted = fmt.Errorf("read interrupted")

// Config holds SSH server configuration.
type Config struct {
	HostKeyPath                string
	Host                       string
	Port                       int
	LegacySSHAlgorithms        bool
	SessionHandler             func(ssh.Session)
	PasswordHandler            func(ctx ssh.Context, password string) bool
	KeyboardInteractiveHandler func(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) bool
	Version                    string // SSH server banner version (default: "Vision3")
	// PublicKeyHandler, when non-nil, is called to authenticate connecting
	// clients by their public key. Return true to allow access.
	PublicKeyHandler func(ctx ssh.Context, key ssh.PublicKey) bool
	// SubsystemHandlers maps SSH subsystem names (e.g. "wfc-admin") to their
	// handler functions. Clients may request a subsystem via the SSH protocol.
	SubsystemHandlers map[string]func(ssh.Session)
}

// Server wraps a gliderlabs/ssh server.
type Server struct {
	inner *ssh.Server
}

// NewServer creates and configures a new SSH server.
func NewServer(cfg Config) (*Server, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	// Read host key
	keyBytes, err := os.ReadFile(cfg.HostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read host key %s: %w", cfg.HostKeyPath, err)
	}
	signer, err := gossh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse host key: %w", err)
	}

	srv := &ssh.Server{
		Addr:            addr,
		Handler:         cfg.SessionHandler,
		HostSigners:     []ssh.Signer{signer},
		PasswordHandler: cfg.PasswordHandler,
		Version:         cfg.Version,
		ConnectionFailedCallback: func(conn net.Conn, err error) {
			slog.Warn("SSH connection failed", "remote", conn.RemoteAddr(), "error", err)
		},
	}
	if cfg.KeyboardInteractiveHandler != nil {
		srv.KeyboardInteractiveHandler = cfg.KeyboardInteractiveHandler
	}
	if cfg.PublicKeyHandler != nil {
		srv.PublicKeyHandler = cfg.PublicKeyHandler
	}
	if len(cfg.SubsystemHandlers) > 0 {
		srv.SubsystemHandlers = make(map[string]ssh.SubsystemHandler, len(cfg.SubsystemHandlers))
		for name, h := range cfg.SubsystemHandlers {
			srv.SubsystemHandlers[name] = ssh.SubsystemHandler(h)
		}
	}

	// Configure algorithm suites via ServerConfigCallback.
	// When LegacySSHAlgorithms is enabled, include older algorithms
	// (diffie-hellman-group1-sha1, 3des-cbc, hmac-sha1, ssh-rsa)
	// required by retro BBS clients.
	legacy := cfg.LegacySSHAlgorithms
	srv.ServerConfigCallback = func(ctx ssh.Context) *gossh.ServerConfig {
		sc := &gossh.ServerConfig{}
		if legacy {
			slog.Debug("SSH legacy algorithms enabled for retro BBS client compatibility")
			sc.KeyExchanges = []string{
				"curve25519-sha256",
				"curve25519-sha256@libssh.org",
				"ecdh-sha2-nistp256",
				"ecdh-sha2-nistp384",
				"ecdh-sha2-nistp521",
				"diffie-hellman-group14-sha256",
				"diffie-hellman-group16-sha512",
				"diffie-hellman-group14-sha1",
				"diffie-hellman-group1-sha1",
			}
			sc.Ciphers = []string{
				"chacha20-poly1305@openssh.com",
				"aes128-gcm@openssh.com",
				"aes256-gcm@openssh.com",
				"aes128-ctr",
				"aes192-ctr",
				"aes256-ctr",
				"aes128-cbc",
				"aes256-cbc",
				"3des-cbc",
			}
			sc.MACs = []string{
				"hmac-sha2-256-etm@openssh.com",
				"hmac-sha2-512-etm@openssh.com",
				"hmac-sha2-256",
				"hmac-sha2-512",
				"hmac-sha1",
			}
		}
		return sc
	}

	return &Server{inner: srv}, nil
}

// ListenAndServe binds to the configured address and serves SSH connections.
// It blocks until the server is closed.
func (s *Server) ListenAndServe() error {
	return s.inner.ListenAndServe()
}

// Close shuts down the server and all active connections.
func (s *Server) Close() error {
	return s.inner.Close()
}

// Cleanup is a no-op retained for API compatibility (was used to call
// ssh_finalize in the old C libssh implementation).
func Cleanup() {}

// readResult holds the outcome of a background read from the SSH channel.
type readResult struct {
	data []byte
	err  error
}

// BBSSession wraps a gliderlabs ssh.Session with SetReadInterrupt support.
// Use WrapSession to create one.
//
// Design invariant: at most ONE goroutine reads from the underlying
// ssh.Session at any time. An interrupted Read leaves its result channel
// available to the next caller, preserving input across transfer/menu handoffs.
type BBSSession struct {
	ssh.Session
	// rawCh is the underlying gossh.Channel extracted from the gliderlabs
	// *session struct. gliderlabs' session.Write() converts \n→\r\n whenever
	// a PTY is accepted, which corrupts binary transfer streams (ZMODEM frame
	// type byte 0x0A becomes 0x0D 0x0A). rawCh.Write() bypasses this.
	rawCh         gossh.Channel
	riMu          sync.Mutex
	readInterrupt <-chan struct{}
	// readChanged wakes an in-progress Read when its interrupt is replaced.
	// readInterrupt and readChanged are protected by riMu.
	readChanged chan struct{}
	// readMu serializes readers and protects readCh and pending. Interrupt
	// updates use riMu instead, so they can wake a blocked reader.
	readMu sync.Mutex
	// readCh retains the one underlying read, even across interruptions.
	readCh chan readResult
	// pending preserves bytes and an accompanying error for smaller reads.
	pending *readResult
	// transferActive is set to 1 during binary file transfers (ZMODEM etc).
	// When active, callers must NOT write to the session (e.g. terminal
	// repaint on window resize) because session.Write() does CRLF conversion
	// that would interleave with RawWrite binary data on the same channel.
	transferActive atomic.Int32
}

// WrapSession wraps a gliderlabs ssh.Session to add BBS-specific features
// (SetReadInterrupt for clean door I/O cancellation, RawWrite for binary
// transfers without CRLF corruption).
func WrapSession(s ssh.Session) *BBSSession {
	bs := &BBSSession{Session: s}
	// Extract the raw gossh.Channel from the gliderlabs session struct so
	// binary transfers can write without the \n→\r\n CRLF conversion that
	// gliderlabs applies when a PTY is accepted.
	bs.rawCh = extractRawChannel(s)
	if bs.rawCh != nil {
		slog.Debug("raw channel extracted for binary transfer support")
	} else {
		slog.Warn("could not extract raw channel; binary transfers may be corrupted by CRLF conversion")
	}
	return bs
}

// extractRawChannel uses reflection to access the gossh.Channel field embedded
// in gliderlabs' unexported *session struct. This raw channel's Write() skips
// the \n→\r\n CRLF normalization that gliderlabs applies when a PTY is accepted,
// making it safe for binary file-transfer protocols (ZMODEM, YMODEM, XMODEM).
// Returns nil if the field cannot be found (e.g. a mock session in tests).
func extractRawChannel(s ssh.Session) gossh.Channel {
	v := reflect.ValueOf(s)
	if !v.IsValid() {
		return nil
	}
	// s is an interface containing *session; dereference the pointer.
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	f := v.FieldByName("Channel")
	if !f.IsValid() || !f.CanInterface() {
		return nil
	}
	ch, ok := f.Interface().(gossh.Channel)
	if !ok || ch == nil {
		return nil
	}
	return ch
}

// RawWrite writes directly to the underlying SSH channel, bypassing
// gliderlabs' \n→\r\n CRLF conversion. Use this for binary data such as
// ZMODEM, YMODEM, or XMODEM frames. Falls back to session.Write() when the
// raw channel is unavailable (e.g. in tests using mock sessions).
func (s *BBSSession) RawWrite(p []byte) (int, error) {
	if s.rawCh != nil {
		return s.rawCh.Write(p)
	}
	return s.Session.Write(p) //nolint:staticcheck // explicit: bypasses BBSSession wrappers
}

// SetTransferActive marks/unmarks the session as being in a binary transfer.
// While active, nothing should write to the session via session.Write()
// (which does CRLF conversion) because it would corrupt the binary stream.
func (s *BBSSession) SetTransferActive(active bool) {
	if active {
		s.transferActive.Store(1)
	} else {
		s.transferActive.Store(0)
	}
}

// IsTransferActive returns true if a binary transfer is in progress.
func (s *BBSSession) IsTransferActive() bool {
	return s.transferActive.Load() != 0
}

// SetReadInterrupt registers a channel that, when closed, causes any
// blocked Read() to return ErrReadInterrupted without consuming data.
// Pass nil to clear the interrupt.
func (s *BBSSession) SetReadInterrupt(ch <-chan struct{}) {
	s.riMu.Lock()
	s.readInterrupt = ch
	if s.readChanged != nil {
		close(s.readChanged)
	}
	s.readChanged = make(chan struct{})
	s.riMu.Unlock()
}

// Read preserves a single underlying SSH read across interruptions. Changes
// to the interrupt wake readers that started with no interrupt, as well as
// readers waiting on a result left behind by an earlier interrupted call.
func (s *BBSSession) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s.readMu.Lock()
	defer s.readMu.Unlock()

	for {
		s.riMu.Lock()
		interrupt := s.readInterrupt
		if s.readChanged == nil {
			s.readChanged = make(chan struct{})
		}
		changed := s.readChanged
		s.riMu.Unlock()

		// An already-fired interrupt must not consume pending input.
		select {
		case <-interrupt:
			return 0, ErrReadInterrupted
		default:
		}

		if s.pending != nil {
			res := s.pending
			n := copy(p, res.data)
			if n < len(res.data) {
				res.data = res.data[n:]
				return n, nil
			}
			s.pending = nil
			return n, res.err
		}

		if s.readCh == nil {
			// A private buffer remains valid if this caller is interrupted.
			buf := make([]byte, len(p))
			ch := make(chan readResult, 1)
			s.readCh = ch
			go func() {
				n, err := s.Session.Read(buf)
				ch <- readResult{data: buf[:n], err: err}
			}()
		}

		select {
		case <-changed:
			// Re-read the current interrupt without abandoning the underlying read.
		case <-interrupt:
			return 0, ErrReadInterrupted
		case res := <-s.readCh:
			s.readCh = nil
			s.pending = &res
			// Check the current interrupt before delivering the received bytes.
		}
	}
}

package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHDialConfig holds the parameters for connecting to the wfc-admin subsystem
// over SSH.
type SSHDialConfig struct {
	// Addr is the host:port of the SSH server.
	Addr string
	// User is the SSH username to authenticate as.
	User string
	// Signer is the client private key used for public-key authentication.
	Signer gossh.Signer
	// KnownHostsPath is the path to a known_hosts file for host key
	// verification. Ignored when Insecure is true.
	KnownHostsPath string
	// Insecure disables host key verification. Only for development/testing;
	// never use in production.
	Insecure bool
	// Timeout bounds the TCP connect and SSH handshake. Zero means 10s.
	Timeout time.Duration
	// KeepAliveInterval and KeepAliveTimeout tune dead-peer detection. Zero
	// selects DefaultKeepAliveInterval / DefaultKeepAliveTimeout; a negative
	// interval disables keepalives (tests only).
	KeepAliveInterval time.Duration
	KeepAliveTimeout  time.Duration
}

// SSHChannelClient is an AdminClient backed by an SSH wfc-admin subsystem
// channel. It embeds *StreamClient for the wire protocol and holds the
// underlying ssh.Client connection so both can be closed together.
type SSHChannelClient struct {
	*StreamClient
	conn      *gossh.Client
	stopKA    func()
	closeOnce sync.Once
	closeErr  error
}

// Close closes the admin stream and the underlying SSH connection. It is
// safe to call more than once.
func (c *SSHChannelClient) Close() error {
	c.closeOnce.Do(func() {
		if c.stopKA != nil {
			c.stopKA()
		}
		streamErr := c.StreamClient.Close()
		connErr := c.conn.Close()
		c.closeErr = errors.Join(streamErr, connErr)
	})
	return c.closeErr
}

// DialSSH connects to the SSH server at cfg.Addr, opens the wfc-admin
// subsystem channel, and returns an AdminClient ready to use. It is
// DialSSHContext with a background context.
func DialSSH(cfg SSHDialConfig) (*SSHChannelClient, error) {
	return DialSSHContext(context.Background(), cfg)
}

// DialSSHContext is DialSSH with a context that can abandon the connect and
// handshake early (the console uses it so a quit during a reconnect attempt
// does not wait out the dial timeout).
//
// Host key verification uses knownhosts.New(cfg.KnownHostsPath) unless
// cfg.Insecure is true, in which case ssh.InsecureIgnoreHostKey() is used.
//
// The returned client sends SSH keepalives so a peer that vanishes without a
// FIN (sleep, network change, crash) is detected within roughly
// KeepAliveInterval+KeepAliveTimeout; the connection is then closed, which
// surfaces through Liveness.Done and as errors from Snapshot/Execute.
func DialSSHContext(ctx context.Context, cfg SSHDialConfig) (*SSHChannelClient, error) {
	var hostKeyCallback gossh.HostKeyCallback
	if cfg.Insecure {
		hostKeyCallback = gossh.InsecureIgnoreHostKey() //nolint:gosec // intentionally insecure in dev
	} else {
		cb, err := knownhosts.New(cfg.KnownHostsPath)
		if err != nil {
			return nil, fmt.Errorf("admin: load known_hosts %q: %w", cfg.KnownHostsPath, err)
		}
		hostKeyCallback = cb
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	clientCfg := &gossh.ClientConfig{
		User: cfg.User,
		Auth: []gossh.AuthMethod{
			gossh.PublicKeys(cfg.Signer),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         timeout,
	}

	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tcp, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("admin: ssh dial %s: %w", cfg.Addr, err)
	}
	// The handshake honours ClientConfig.Timeout via a deadline on tcp, but
	// not ctx; closing the socket on cancellation is what aborts it early.
	// The watcher is joined before this function returns: the deferred
	// cancel would otherwise race a watcher that has not woken yet, which
	// could pick the cancellation case and close a live connection.
	handshakeDone := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-dialCtx.Done():
			_ = tcp.Close() // aborts a handshake that is still in progress
		case <-handshakeDone:
		}
	}()
	sshConn, chans, reqs, err := gossh.NewClientConn(tcp, cfg.Addr, clientCfg)
	close(handshakeDone)
	<-watcherDone
	if err == nil && dialCtx.Err() != nil {
		// The caller cancelled during the handshake and the watcher may
		// have picked that case and closed tcp; report the cancellation
		// rather than letting session setup fail on a dead transport.
		_ = sshConn.Close()
		return nil, fmt.Errorf("admin: ssh dial %s: %w", cfg.Addr, ctx.Err())
	}
	if err != nil {
		_ = tcp.Close() // cleanup on error path
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("admin: ssh dial %s: %w", cfg.Addr, ctxErr)
		}
		return nil, fmt.Errorf("admin: ssh dial %s: %w", cfg.Addr, err)
	}
	conn := gossh.NewClient(sshConn, chans, reqs)

	sess, err := conn.NewSession()
	if err != nil {
		_ = conn.Close() // cleanup on error path
		return nil, fmt.Errorf("admin: ssh new session: %w", err)
	}

	if err := sess.RequestSubsystem("wfc-admin"); err != nil {
		_ = sess.Close() // cleanup on error path
		_ = conn.Close() // cleanup on error path
		return nil, fmt.Errorf("admin: request wfc-admin subsystem: %w", err)
	}

	// Combine stdin+stdout into a single ReadWriteCloser for the stream client.
	rwc := &sshSessionRWC{sess: sess}
	streamClient := NewStreamClient(rwc)

	client := &SSHChannelClient{
		StreamClient: streamClient,
		conn:         conn,
	}
	if cfg.KeepAliveInterval >= 0 {
		client.stopKA = KeepAlive(conn, cfg.KeepAliveInterval, cfg.KeepAliveTimeout, func(error) {
			// Closing the SSH connection makes the session reader return, so
			// the StreamClient read loop exits and Done() fires.
			_ = conn.Close()
		})
	}
	return client, nil
}

// sshSessionRWC wraps an *gossh.Session, combining its stdin writer and
// stdout reader into a single io.ReadWriteCloser for the stream protocol.
type sshSessionRWC struct {
	sess    *gossh.Session
	once    sync.Once
	stdin   io.WriteCloser
	stdout  io.Reader
	openErr error
}

func (s *sshSessionRWC) ensureOpen() error {
	s.once.Do(func() {
		s.stdin, s.openErr = s.sess.StdinPipe()
		if s.openErr != nil {
			s.openErr = fmt.Errorf("admin: ssh stdin pipe: %w", s.openErr)
			return
		}
		s.stdout, s.openErr = s.sess.StdoutPipe()
		if s.openErr != nil {
			s.openErr = fmt.Errorf("admin: ssh stdout pipe: %w", s.openErr)
		}
	})
	return s.openErr
}

func (s *sshSessionRWC) Read(p []byte) (int, error) {
	if err := s.ensureOpen(); err != nil {
		return 0, err
	}
	return s.stdout.Read(p)
}

func (s *sshSessionRWC) Write(p []byte) (int, error) {
	if err := s.ensureOpen(); err != nil {
		return 0, err
	}
	return s.stdin.Write(p)
}

func (s *sshSessionRWC) Close() error {
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	return s.sess.Close()
}

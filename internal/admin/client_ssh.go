package admin

import (
	"fmt"
	"io"

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
}

// SSHChannelClient is an AdminClient backed by an SSH wfc-admin subsystem
// channel. It embeds *StreamClient for the wire protocol and holds the
// underlying ssh.Client connection so both can be closed together.
type SSHChannelClient struct {
	*StreamClient
	conn *gossh.Client
}

// Close closes the admin stream and the underlying SSH connection.
func (c *SSHChannelClient) Close() error {
	streamErr := c.StreamClient.Close()
	connErr := c.conn.Close()
	if streamErr != nil {
		return streamErr
	}
	return connErr
}

// DialSSH connects to the SSH server at cfg.Addr, opens the wfc-admin
// subsystem channel, and returns an AdminClient ready to use.
//
// Host key verification uses knownhosts.New(cfg.KnownHostsPath) unless
// cfg.Insecure is true, in which case ssh.InsecureIgnoreHostKey() is used.
func DialSSH(cfg SSHDialConfig) (*SSHChannelClient, error) {
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

	clientCfg := &gossh.ClientConfig{
		User: cfg.User,
		Auth: []gossh.AuthMethod{
			gossh.PublicKeys(cfg.Signer),
		},
		HostKeyCallback: hostKeyCallback,
	}

	conn, err := gossh.Dial("tcp", cfg.Addr, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("admin: ssh dial %s: %w", cfg.Addr, err)
	}

	sess, err := conn.NewSession()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("admin: ssh new session: %w", err)
	}

	if err := sess.RequestSubsystem("wfc-admin"); err != nil {
		sess.Close()
		conn.Close()
		return nil, fmt.Errorf("admin: request wfc-admin subsystem: %w", err)
	}

	// Combine stdin+stdout into a single ReadWriteCloser for the stream client.
	rwc := &sshSessionRWC{sess: sess}
	streamClient := NewStreamClient(rwc)

	return &SSHChannelClient{
		StreamClient: streamClient,
		conn:         conn,
	}, nil
}

// sshSessionRWC wraps an *gossh.Session, combining its stdin writer and
// stdout reader into a single io.ReadWriteCloser for the stream protocol.
type sshSessionRWC struct {
	sess   *gossh.Session
	stdin  io.WriteCloser
	stdout io.Reader
	opened bool
}

func (s *sshSessionRWC) ensureOpen() error {
	if s.opened {
		return nil
	}
	var err error
	s.stdin, err = s.sess.StdinPipe()
	if err != nil {
		return fmt.Errorf("admin: ssh stdin pipe: %w", err)
	}
	s.stdout, err = s.sess.StdoutPipe()
	if err != nil {
		return fmt.Errorf("admin: ssh stdout pipe: %w", err)
	}
	s.opened = true
	return nil
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

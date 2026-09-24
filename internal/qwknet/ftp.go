package qwknet

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// errNoSuchFile is returned by retrieve when the server has nothing under
// that name (550), which for a hub means "no packet for you right now".
var errNoSuchFile = errors.New("file not available")

// ftpClient is the small slice of RFC 959 a QWK network node needs: log in,
// switch to binary, and move one file each way in passive mode. Hubs are
// almost always Synchronet, whose FTP server builds the node's QWK packet
// when the node asks for <HUBID>.QWK and imports <HUBID>.REP on upload.
type ftpClient struct {
	conn    net.Conn
	r       *bufio.Reader
	host    string // control-connection host, for PASV replies that say 0.0.0.0
	timeout time.Duration
	dialer  net.Dialer
	// broken is set once the control connection fails to send or answer.
	// Later commands then fail at once instead of each waiting out the
	// timeout, so a caller can try the next step without knowing the state.
	broken bool
}

// errConnBroken is returned by commands issued after the control connection
// has already failed.
var errConnBroken = errors.New("control connection lost")

// ftpDial connects and consumes the greeting.
func ftpDial(ctx context.Context, hostPort string, timeout time.Duration) (*ftpClient, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", hostPort)
	if err != nil {
		return nil, err
	}
	host, _, _ := net.SplitHostPort(hostPort)
	c := &ftpClient{conn: conn, r: bufio.NewReader(conn), host: host, timeout: timeout, dialer: d}
	code, msg, err := c.readReply()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("reading greeting: %w", err)
	}
	if code != 220 {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected greeting: %d %s", code, msg)
	}
	return c, nil
}

// errBadArgument is returned for a credential or file name that could not
// be sent as one FTP command line.
var errBadArgument = errors.New("value contains control characters")

// checkArgument rejects values that would inject a second command: FTP is
// line-oriented, so a CR or LF (or any other control byte) in a user name,
// password or file name ends the command early.
func checkArgument(v string) error {
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return errBadArgument
		}
	}
	return nil
}

// login runs USER/PASS and switches to binary mode.
func (c *ftpClient) login(user, pass string) error {
	if err := checkArgument(user); err != nil {
		return fmt.Errorf("user name: %w", err)
	}
	if err := checkArgument(pass); err != nil {
		return fmt.Errorf("password: %w", err)
	}
	code, msg, err := c.cmd("USER %s", user)
	if err != nil {
		return err
	}
	if code == 331 {
		code, msg, err = c.cmd("PASS %s", pass)
		if err != nil {
			return err
		}
	}
	if code != 230 {
		return fmt.Errorf("login refused: %d %s", code, msg)
	}
	if code, msg, err = c.cmd("TYPE I"); err != nil {
		return err
	} else if code/100 != 2 {
		return fmt.Errorf("TYPE I refused: %d %s", code, msg)
	}
	return nil
}

// quit ends the session politely; errors are ignored since the transfers
// that mattered have already completed.
func (c *ftpClient) quit() {
	_, _, _ = c.cmd("QUIT")
	_ = c.conn.Close()
}

// cmd sends one command and returns the reply.
func (c *ftpClient) cmd(format string, args ...any) (int, string, error) {
	line := fmt.Sprintf(format, args...)
	if c.broken {
		return 0, "", fmt.Errorf("sending %s: %w", firstWord(line), errConnBroken)
	}
	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))
	if _, err := io.WriteString(c.conn, line+"\r\n"); err != nil {
		c.broken = true
		return 0, "", fmt.Errorf("sending %s: %w", firstWord(line), err)
	}
	code, msg, err := c.readReply()
	if err != nil {
		return 0, "", fmt.Errorf("reply to %s: %w", firstWord(line), err)
	}
	return code, msg, nil
}

// readReply reads a single- or multi-line reply ("123-" ... "123 "). Any
// failure leaves the reply stream out of step, so it marks the client broken.
func (c *ftpClient) readReply() (int, string, error) {
	code, text, err := c.readReplyRaw()
	if err != nil {
		c.broken = true
	}
	return code, text, err
}

func (c *ftpClient) readReplyRaw() (int, string, error) {
	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))
	line, err := c.r.ReadString('\n')
	if err != nil {
		return 0, "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) < 3 {
		return 0, "", fmt.Errorf("short reply %q", line)
	}
	code, err := strconv.Atoi(line[:3])
	if err != nil {
		return 0, "", fmt.Errorf("bad reply %q", line)
	}
	text := strings.TrimSpace(line[3:])
	if len(line) > 3 && line[3] == '-' {
		prefix := line[:3] + " "
		for {
			more, err := c.r.ReadString('\n')
			if err != nil {
				return 0, "", err
			}
			more = strings.TrimRight(more, "\r\n")
			text += "\n" + more
			if strings.HasPrefix(more, prefix) {
				break
			}
		}
	}
	return code, text, nil
}

// passive opens a data connection via EPSV, falling back to PASV.
func (c *ftpClient) passive(ctx context.Context) (net.Conn, error) {
	if code, msg, err := c.cmd("EPSV"); err == nil && code == 229 {
		if port, ok := parseEPSV(msg); ok {
			return c.dialer.DialContext(ctx, "tcp", net.JoinHostPort(c.host, strconv.Itoa(port)))
		}
	}
	code, msg, err := c.cmd("PASV")
	if err != nil {
		return nil, err
	}
	if code != 227 {
		return nil, fmt.Errorf("PASV refused: %d %s", code, msg)
	}
	host, port, ok := parsePASV(msg)
	if !ok {
		return nil, fmt.Errorf("unparsable PASV reply %q", msg)
	}
	if host == "0.0.0.0" || host == "" {
		host = c.host
	}
	return c.dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
}

// parsePASV reads "(h1,h2,h3,h4,p1,p2)" from a 227 reply.
func parsePASV(msg string) (string, int, bool) {
	start := strings.IndexByte(msg, '(')
	end := strings.IndexByte(msg, ')')
	if start < 0 || end < start {
		return "", 0, false
	}
	parts := strings.Split(msg[start+1:end], ",")
	if len(parts) != 6 {
		return "", 0, false
	}
	nums := make([]int, 6)
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 || n > 255 {
			return "", 0, false
		}
		nums[i] = n
	}
	host := fmt.Sprintf("%d.%d.%d.%d", nums[0], nums[1], nums[2], nums[3])
	return host, nums[4]<<8 | nums[5], true
}

// parseEPSV reads "(|||port|)" from a 229 reply.
func parseEPSV(msg string) (int, bool) {
	start := strings.IndexByte(msg, '(')
	end := strings.IndexByte(msg, ')')
	if start < 0 || end < start {
		return 0, false
	}
	inner := strings.Trim(msg[start+1:end], "|")
	port, err := strconv.Atoi(inner)
	if err != nil || port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

// store uploads r as name.
func (c *ftpClient) store(ctx context.Context, name string, r io.Reader) error {
	if err := checkArgument(name); err != nil {
		return fmt.Errorf("file name %q: %w", name, err)
	}
	data, err := c.passive(ctx)
	if err != nil {
		return fmt.Errorf("opening data connection: %w", err)
	}
	code, msg, err := c.cmd("STOR %s", name)
	if err != nil {
		_ = data.Close()
		return err
	}
	if code/100 != 1 {
		_ = data.Close()
		return fmt.Errorf("STOR %s refused: %d %s", name, code, msg)
	}
	_ = data.SetDeadline(time.Now().Add(c.timeout))
	_, copyErr := io.Copy(data, r)
	closeErr := data.Close()
	code, msg, err = c.readReply()
	if copyErr != nil {
		return fmt.Errorf("uploading %s: %w", name, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing upload of %s: %w", name, closeErr)
	}
	if err != nil {
		return fmt.Errorf("after STOR %s: %w", name, err)
	}
	if code/100 != 2 {
		return fmt.Errorf("STOR %s failed: %d %s", name, code, msg)
	}
	return nil
}

// retrieve downloads name into w and returns the byte count. A 550 (or 450)
// reply to RETR is reported as errNoSuchFile.
func (c *ftpClient) retrieve(ctx context.Context, name string, w io.Writer) (int64, error) {
	if err := checkArgument(name); err != nil {
		return 0, fmt.Errorf("file name %q: %w", name, err)
	}
	data, err := c.passive(ctx)
	if err != nil {
		return 0, fmt.Errorf("opening data connection: %w", err)
	}
	code, msg, err := c.cmd("RETR %s", name)
	if err != nil {
		_ = data.Close()
		return 0, err
	}
	if code/100 != 1 {
		_ = data.Close()
		if code == 550 || code == 450 {
			return 0, errNoSuchFile
		}
		return 0, fmt.Errorf("RETR %s refused: %d %s", name, code, msg)
	}
	_ = data.SetDeadline(time.Now().Add(c.timeout))
	n, copyErr := io.Copy(w, data)
	_ = data.Close()
	code, msg, err = c.readReply()
	if copyErr != nil {
		return n, fmt.Errorf("downloading %s: %w", name, copyErr)
	}
	if err != nil {
		return n, fmt.Errorf("after RETR %s: %w", name, err)
	}
	if code/100 != 2 {
		return n, fmt.Errorf("RETR %s failed: %d %s", name, code, msg)
	}
	return n, nil
}

func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return s
}

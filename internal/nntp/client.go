package nntp

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrMissing reports an article the server no longer has.
var ErrMissing = errors.New("NNTP article expired or unavailable")

// ResponseError is a status line from the server that the client did not
// expect for the command it sent.
type ResponseError struct {
	Code int
	Text string
}

func (e *ResponseError) Error() string { return fmt.Sprintf("NNTP %d: %s", e.Code, e.Text) }

// Overview is one row of an OVER listing: the article number in its group,
// its Message-ID and its size in bytes.
type Overview struct {
	Number int64
	ID     string
	Bytes  int64
}

// Reader is the part of an NNTP reading session the importer uses. Client
// implements it; tests supply their own.
type Reader interface {
	Group(string) (int64, int64, error)
	Overview(int64, int64) ([]Overview, error)
	Article(int64) ([]byte, error)
	Close() error
}

// Client is a read-only NNTP session with one server. It is not safe for
// concurrent use.
type Client struct {
	conn     net.Conn
	r        *bufio.Reader
	ctx      context.Context
	timeout  time.Duration
	maxBytes int
	stop     func() bool
}

// Dial connects to the server in c, over TLS when c.TLS is set, and reads its
// greeting. Cancelling ctx closes the connection.
func Dial(ctx context.Context, c Config) (*Client, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	d := &net.Dialer{Timeout: c.Timeout()}
	var conn net.Conn
	var err error
	if c.TLS {
		td := &tls.Dialer{NetDialer: d, Config: &tls.Config{MinVersion: tls.VersionTLS12}}
		conn, err = td.DialContext(ctx, "tcp", c.Server)
	} else {
		conn, err = d.DialContext(ctx, "tcp", c.Server)
	}
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", c.Server, err)
	}
	cl := &Client{conn: conn, r: bufio.NewReader(conn), ctx: ctx, timeout: c.Timeout(), maxBytes: c.MaxArticleBytes}
	cl.stop = context.AfterFunc(ctx, func() { _ = conn.Close() })
	cl.deadline()
	code, text, err := cl.response()
	if err == nil && code != 200 && code != 201 {
		err = &ResponseError{code, text}
	}
	if err == nil {
		code, text, err = cl.command("MODE READER")
		if err == nil && code != 200 && code != 201 && code != 500 && code != 501 {
			err = &ResponseError{code, text}
		}
	}
	if err != nil {
		_ = cl.Close()
		return nil, err
	}
	return cl, nil
}

// Close ends the session with QUIT and closes the connection.
func (c *Client) Close() error {
	if c.stop != nil {
		c.stop()
	}
	return c.conn.Close()
}
func (c *Client) deadline() {
	d := time.Now().Add(c.timeout)
	if end, ok := c.ctx.Deadline(); ok && end.Before(d) {
		d = end
	}
	_ = c.conn.SetDeadline(d)
}
func (c *Client) line() (string, error) {
	var b []byte
	for {
		part, err := c.r.ReadSlice('\n')
		if len(b)+len(part) > 65536 {
			_ = c.conn.Close()
			return "", fmt.Errorf("NNTP response line exceeds 64 KiB")
		}
		b = append(b, part...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return "", err
		}
		break
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r"), nil
}
func (c *Client) response() (int, string, error) {
	line, err := c.line()
	if err != nil {
		return 0, "", err
	}
	if len(line) < 3 {
		return 0, "", fmt.Errorf("invalid NNTP response")
	}
	code, err := strconv.Atoi(line[:3])
	if err != nil || code < 100 || code > 599 {
		return 0, "", fmt.Errorf("invalid NNTP response code")
	}
	return code, strings.TrimSpace(line[3:]), nil
}
func (c *Client) command(s string) (int, string, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, "", err
	}
	c.deadline()
	if _, err := fmt.Fprintf(c.conn, "%s\r\n", s); err != nil {
		return 0, "", err
	}
	return c.response()
}

// Group selects group and returns its lowest and highest article numbers.
func (c *Client) Group(group string) (int64, int64, error) {
	if !validGroup(group) {
		return 0, 0, fmt.Errorf("invalid group %q", group)
	}
	code, text, err := c.command("GROUP " + group)
	if err != nil {
		return 0, 0, err
	}
	if code != 211 {
		return 0, 0, &ResponseError{code, text}
	}
	f := strings.Fields(text)
	if len(f) < 4 {
		return 0, 0, fmt.Errorf("invalid GROUP response")
	}
	low, e1 := strconv.ParseInt(f[1], 10, 64)
	high, e2 := strconv.ParseInt(f[2], 10, 64)
	if e1 != nil || e2 != nil || low < 0 || high < 0 || f[3] != group {
		return 0, 0, fmt.Errorf("invalid GROUP bounds")
	}
	return low, high, nil
}
func (c *Client) block(limit int) ([]byte, error) {
	var out []byte
	for {
		line, err := c.line()
		if err != nil {
			return nil, err
		}
		if line == "." {
			return out, nil
		}
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		if len(out)+len(line)+2 > limit {
			_ = c.conn.Close()
			return nil, fmt.Errorf("NNTP data exceeds %d bytes", limit)
		}
		out = append(out, line...)
		out = append(out, '\r', '\n')
	}
}

// Overview lists the articles numbered low to high in the selected group. A
// range covers at most 100 articles.
func (c *Client) Overview(low, high int64) ([]Overview, error) {
	if low < 1 || high < low || high-low >= 100 {
		return nil, fmt.Errorf("invalid overview range")
	}
	command := fmt.Sprintf("OVER %d-%d", low, high)
	code, text, err := c.command(command)
	if err == nil && (code == 500 || code == 501) {
		code, text, err = c.command(fmt.Sprintf("XOVER %d-%d", low, high))
	}
	if err != nil {
		return nil, err
	}
	if code == 423 {
		return nil, nil
	}
	if code != 224 {
		return nil, &ResponseError{code, text}
	}
	data, err := c.block(1048576)
	if err != nil {
		return nil, err
	}
	var items []Overview
	seen := map[int64]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(strings.TrimSuffix(line, "\r"), "\t")
		if len(f) < 7 {
			return nil, fmt.Errorf("invalid OVER row")
		}
		n, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil || n < low || n > high || seen[n] {
			return nil, fmt.Errorf("invalid/duplicate OVER article number")
		}
		seen[n] = true
		size, _ := strconv.ParseInt(f[6], 10, 64)
		items = append(items, Overview{n, strings.TrimSpace(f[4]), size})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Number < items[j].Number })
	return items, nil
}

// Article fetches the raw article with that number from the selected group.
// It returns ErrMissing when the server no longer has it.
func (c *Client) Article(number int64) ([]byte, error) {
	if number < 1 {
		return nil, fmt.Errorf("invalid article number")
	}
	code, text, err := c.command(fmt.Sprintf("ARTICLE %d", number))
	if err != nil {
		return nil, err
	}
	if code == 423 || code == 430 {
		return nil, ErrMissing
	}
	if code != 220 {
		return nil, &ResponseError{code, text}
	}
	return c.block(c.maxBytes)
}

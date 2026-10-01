package admin

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
)

// SnoopRequest is the first line a wfc-snoop client sends.
type SnoopRequest struct {
	NodeID      int       `json:"node"`
	ConnectedAt time.Time `json:"connectedAt"`
}

// SnoopHeader is the server's one-line reply. A non-empty Error means the
// request was refused and the stream ends.
type SnoopHeader struct {
	Error      string `json:"error,omitempty"`
	OutputMode string `json:"outputMode"` // "cp437" or "utf8"
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Handle     string `json:"handle"`
}

// SnoopTarget resolves a request to the node's tap and header fields.
type SnoopTarget func(req SnoopRequest) (*snoop.Tap, SnoopHeader, error)

// SnoopStream is the client end: the header, then raw node output to read
// and sysop input to write.
type SnoopStream struct {
	Header SnoopHeader
	io.ReadWriteCloser
	r *bufio.Reader
}

// NewSnoopStream wraps an already-handshaken connection as a SnoopStream.
func NewSnoopStream(h SnoopHeader, rwc io.ReadWriteCloser) *SnoopStream {
	return &SnoopStream{Header: h, ReadWriteCloser: rwc, r: bufio.NewReader(rwc)}
}

func (s *SnoopStream) Read(p []byte) (int, error) { return s.r.Read(p) }

const maxSnoopLine = 4 << 10

func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return nil, fmt.Errorf("admin: snoop line exceeds %d bytes", maxSnoopLine)
	}
	return line, err
}

// ServeSnoop runs the server side of one wfc-snoop channel for sysop.
func ServeSnoop(rw io.ReadWriteCloser, sysop string, resolve SnoopTarget, audit func(msg string, args ...any)) error {
	// Closing also unblocks the input goroutine's read; the channel is
	// finished either way, so its close error is not actionable.
	defer func() { _ = rw.Close() }()
	br := bufio.NewReaderSize(rw, maxSnoopLine)
	line, err := readLine(br)
	if err != nil {
		return err
	}
	var req SnoopRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return writeHeader(rw, SnoopHeader{Error: "bad request"})
	}
	tap, hdr, err := resolve(req)
	if err != nil {
		_ = writeHeader(rw, SnoopHeader{Error: err.Error()})
		return err
	}
	// Attach before the header so a client that has the header can take
	// the keyboard at once. The catch-up chunk waits in the watcher queue
	// until the loop below runs.
	w := tap.AttachAs(sysop)
	if err := writeHeader(rw, hdr); err != nil {
		w.Close()
		return err
	}
	start := time.Now()
	audit("snoop attach", "sysop", sysop, "node", req.NodeID, "caller", hdr.Handle)
	defer func() {
		// Release first for the held/injected counts; closing the last
		// watch would release the keyboard without reporting them.
		if held, injected, ok := tap.ReleaseKeyboard(sysop); ok {
			audit("type-in off", "sysop", sysop, "node", req.NodeID,
				"duration", held.Round(time.Second), "bytes", injected)
		}
		w.Close()
		audit("snoop detach", "sysop", sysop, "node", req.NodeID, "caller", hdr.Handle,
			"duration", time.Since(start).Round(time.Second))
	}()

	inErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 512)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				tap.Inject(sysop, buf[:n])
			}
			if err != nil {
				inErr <- err
				return
			}
		}
	}()

	for {
		select {
		case chunk, ok := <-w.C():
			if !ok {
				return nil // caller left
			}
			if _, err := rw.Write(chunk); err != nil {
				return err
			}
		case err := <-inErr:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// WriteSnoopError refuses a snoop channel with a header carrying msg, the
// way ServeSnoop refuses a request it cannot serve.
func WriteSnoopError(w io.Writer, msg string) error {
	return writeHeader(w, SnoopHeader{Error: msg})
}

// RefuseSnoop reads the client's request line, waiting at most wait, then
// refuses the channel with msg. The client writes its request before reading
// the header, so a channel closed without reading it can fail that write and
// lose msg.
func RefuseSnoop(rw io.ReadWriter, msg string, wait time.Duration) error {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = readLine(bufio.NewReaderSize(rw, maxSnoopLine))
	}()
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
	}
	return WriteSnoopError(rw, msg)
}

func writeHeader(w io.Writer, h SnoopHeader) error {
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// ClientSnoop performs the client handshake on rw.
func ClientSnoop(rw io.ReadWriteCloser, req SnoopRequest) (*SnoopStream, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := rw.Write(append(b, '\n')); err != nil {
		_ = rw.Close()
		return nil, err
	}
	br := bufio.NewReaderSize(rw, maxSnoopLine)
	line, err := readLine(br)
	if err != nil {
		_ = rw.Close()
		return nil, fmt.Errorf("admin: snoop handshake: %w", err)
	}
	var hdr SnoopHeader
	if err := json.Unmarshal(line, &hdr); err != nil {
		_ = rw.Close()
		return nil, fmt.Errorf("admin: snoop header: %w", err)
	}
	if hdr.Error != "" {
		_ = rw.Close()
		return nil, errors.New(hdr.Error)
	}
	return &SnoopStream{Header: hdr, ReadWriteCloser: rw, r: br}, nil
}

package admin

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// ServeRPC runs the server side of the admin protocol over rw. It sends an
// initial snapshot, then concurrently streams events and answers commands
// until ctx is cancelled or the stream errors. audit, if non-nil, is called
// with a short description of each command for slog auditing.
//
// rw must be closable (e.g. net.Conn or ssh.Session); ServeRPC closes it when
// the event-streaming goroutine fails so that the outer ReadFrame unblocks.
func ServeRPC(ctx context.Context, rw io.ReadWriteCloser, srv *Server, audit func(string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var writeMu sync.Mutex
	write := func(f *Frame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return WriteFrame(rw, f)
	}

	// Ensure the server has a snapshot before sending it.
	// If srv.Snapshot() is nil (first tick not yet scheduled), force one tick
	// so the snapshot is guaranteed non-nil on the next read.
	initialSnap := srv.Snapshot()
	if initialSnap == nil {
		srv.tick(timeNow())
		initialSnap = srv.Snapshot()
	}
	if err := write(&Frame{Kind: KindSnapshot, Snapshot: initialSnap}); err != nil {
		return err
	}

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := srv.Subscribe(subCtx)
	go func() {
		for e := range events {
			ev := e
			if err := write(&Frame{Kind: KindEvent, Event: &ev}); err != nil {
				cancel()
				rw.Close() // unblock the outer ReadFrame
				return
			}
		}
	}()

	for {
		f, err := ReadFrame(rw)
		if err != nil {
			return err
		}
		if f.Kind != KindCommand || f.Command == nil {
			continue
		}
		if audit != nil {
			audit(string(f.Command.Command))
		}
		res, err := srv.Execute(*f.Command)
		out := &Frame{Kind: KindResult}
		if err != nil {
			out.Kind = KindError
			out.Err = err.Error()
		} else {
			out.Result = res
		}
		if werr := write(out); werr != nil {
			return werr
		}
	}
}

// StreamClient is the client engine for the admin protocol over a stream.
// SSHChannelClient wraps it around an SSH channel.
type StreamClient struct {
	rwc       io.ReadWriteCloser
	mu        sync.Mutex
	snap      *SystemSnapshot
	snapReady chan struct{} // closed exactly once: first snapshot OR readLoop exit
	snapOnce  sync.Once
	execMu    sync.Mutex // serialises Execute: one command/response in flight
	results   chan *Frame
	events    chan Event
	closeFn   func() error
	once      sync.Once
}

// NewStreamClient starts the read loop over rwc.
func NewStreamClient(rwc io.ReadWriteCloser) *StreamClient {
	c := &StreamClient{
		rwc:       rwc,
		snapReady: make(chan struct{}),
		results:   make(chan *Frame, 4),
		events:    make(chan Event, 256),
		closeFn:   rwc.Close,
	}
	go c.readLoop()
	return c
}

func (c *StreamClient) readLoop() {
	// Always unblock Snapshot() callers when readLoop exits, whether cleanly or
	// on error — so snapReady is closed exactly once regardless of path.
	defer c.snapOnce.Do(func() { close(c.snapReady) })

	for {
		f, err := ReadFrame(c.rwc)
		if err != nil {
			close(c.events)
			return
		}
		switch f.Kind {
		case KindSnapshot:
			if f.Snapshot != nil {
				c.mu.Lock()
				c.snap = f.Snapshot
				c.mu.Unlock()
				c.snapOnce.Do(func() { close(c.snapReady) })
			}
		case KindEvent:
			if f.Event != nil {
				select {
				case c.events <- *f.Event:
				default:
				}
			}
		case KindResult, KindError:
			select {
			case c.results <- f:
			default:
			}
		}
	}
}

// Snapshot waits for the initial snapshot from the server and returns it.
// Subsequent calls return the most recently received snapshot immediately.
// Returns an error if the connection closes before a snapshot is received.
func (c *StreamClient) Snapshot(ctx context.Context) (*SystemSnapshot, error) {
	select {
	case <-c.snapReady:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snap == nil {
		return nil, fmt.Errorf("admin: connection closed before snapshot received")
	}
	return c.snap, nil
}

// Subscribe returns the live event channel populated by the read loop.
func (c *StreamClient) Subscribe(ctx context.Context) (<-chan Event, error) {
	return c.events, nil
}

// Execute sends a command and waits for the result frame from the server.
// Only one Execute is allowed in flight at a time; concurrent callers queue.
func (c *StreamClient) Execute(ctx context.Context, cmd AdminCommand) (*Result, error) {
	c.execMu.Lock()
	defer c.execMu.Unlock()

	if err := WriteFrame(c.rwc, &Frame{Kind: KindCommand, Command: &cmd}); err != nil {
		return nil, err
	}
	select {
	case f := <-c.results:
		if f.Kind == KindError {
			return nil, errFromString(f.Err)
		}
		return f.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close closes the underlying connection exactly once.
func (c *StreamClient) Close() error {
	var err error
	c.once.Do(func() { err = c.closeFn() })
	return err
}

func errFromString(s string) error { return &rpcError{s} }

type rpcError struct{ msg string }

func (e *rpcError) Error() string { return e.msg }

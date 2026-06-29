package admin

import (
	"context"
	"io"
	"sync"
)

// ServeRPC runs the server side of the admin protocol over rw. It sends an
// initial snapshot, then concurrently streams events and answers commands
// until ctx is cancelled or the stream errors. audit, if non-nil, is called
// with a short description of each command for slog auditing.
func ServeRPC(ctx context.Context, rw io.ReadWriter, srv *Server, audit func(string)) error {
	var writeMu sync.Mutex
	write := func(f *Frame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return WriteFrame(rw, f)
	}

	// Wait for the server to have a non-nil snapshot before sending it.
	// srv.Run ticks once immediately but goroutine scheduling may delay it.
	var initialSnap *SystemSnapshot
	for initialSnap == nil {
		initialSnap = srv.Snapshot()
		if initialSnap == nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				// yield and retry
			}
		}
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
	snapReady chan struct{} // closed once on first snapshot receipt
	snapOnce  sync.Once
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
func (c *StreamClient) Snapshot(ctx context.Context) (*SystemSnapshot, error) {
	select {
	case <-c.snapReady:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap, nil
}

// Subscribe returns the live event channel populated by the read loop.
func (c *StreamClient) Subscribe(ctx context.Context) (<-chan Event, error) {
	return c.events, nil
}

// Execute sends a command and waits for the result frame from the server.
func (c *StreamClient) Execute(ctx context.Context, cmd AdminCommand) (*Result, error) {
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

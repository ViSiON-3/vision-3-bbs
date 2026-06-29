package admin

import "context"

// InProcessClient adapts a *Server to AdminClient for daemon-embedded use
// (the --wfc local console and the server-side SSH admin TUI). No serialization.
type InProcessClient struct{ srv *Server }

// NewInProcessClient wraps a running Server.
func NewInProcessClient(srv *Server) *InProcessClient { return &InProcessClient{srv: srv} }

func (c *InProcessClient) Snapshot(ctx context.Context) (*SystemSnapshot, error) {
	if snap := c.srv.Snapshot(); snap != nil {
		return snap, nil
	}
	c.srv.tick(timeNow())
	return c.srv.Snapshot(), nil
}

func (c *InProcessClient) Subscribe(ctx context.Context) (<-chan Event, error) {
	return c.srv.Subscribe(ctx), nil
}

func (c *InProcessClient) Execute(ctx context.Context, cmd AdminCommand) (*Result, error) {
	return c.srv.Execute(cmd)
}

func (c *InProcessClient) Close() error { return nil }

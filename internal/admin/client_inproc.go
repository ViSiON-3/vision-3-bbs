package admin

import "context"

// InProcessClient adapts a *Server to the AdminClient interface without any
// network serialization. It is intentional architecture, NOT dead code.
//
// Design context: the spec defines two consumer paths for AdminClient —
// (1) the daemon-embedded local console launched via the --wfc flag, and
// (2) the server-side SSH admin TUI — both of which run in-process against the
// daemon and therefore use InProcessClient instead of the gRPC/net transport.
// These consumers are deferred to a later phase; InProcessClient is the
// pre-wired adapter that will slot in when that phase lands.
type InProcessClient struct{ srv *Server }

// NewInProcessClient wraps a running Server so it can be consumed via the
// AdminClient interface in-process (no serialization, no network hop).
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

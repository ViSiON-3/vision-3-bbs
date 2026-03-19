package v3net

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// JAMRouter implements leaf.JAMWriter and dispatches messages to the
// correct per-area JAMAdapter based on the message's AreaTag.
type JAMRouter struct {
	adapters map[string]leaf.JAMWriter
}

// NewJAMRouter creates a new JAMRouter with an empty set of adapters.
func NewJAMRouter() *JAMRouter {
	return &JAMRouter{adapters: make(map[string]leaf.JAMWriter)}
}

// Add registers a JAMWriter for the given area tag.
func (r *JAMRouter) Add(areaTag string, writer leaf.JAMWriter) {
	r.adapters[areaTag] = writer
}

// WriteMessage dispatches the message to the appropriate area adapter.
// If no adapter exists for the message's AreaTag, it logs a warning and returns 0, nil.
func (r *JAMRouter) WriteMessage(msg protocol.Message) (int64, error) {
	adapter, ok := r.adapters[msg.AreaTag]
	if !ok {
		slog.Warn("v3net: no local area for tag, skipping", "area_tag", msg.AreaTag)
		return 0, nil
	}
	return adapter.WriteMessage(msg)
}

package v3net

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// V3Net Central activity alerts through the service. The BBS decides which
// of its callers' actions may be announced (SendCentralAlert) and who sees
// the alerts other boards send (SetCentralAlertSink); each leaf applies its
// sysop's switches and the hub's support. One event sent on several
// networks carries one ID, and a board on more than one of them hears it
// once: the service hands the sink only the first copy.

// serviceAlertsSeen is how many alerts the service remembers across its
// leaves, to drop the copies of one event that arrive on other networks.
const serviceAlertsSeen = 2048

// alertDedup remembers the alerts handed to the sink, by node and ID.
type alertDedup struct {
	mu    sync.Mutex
	seen  map[string]bool
	order []string
}

// claim records node's alert id and reports whether it is new.
func (d *alertDedup) claim(node, id string) bool {
	key := node + "\x00" + id
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen[key] {
		return false
	}
	if d.seen == nil {
		d.seen = make(map[string]bool)
	}
	if len(d.order) >= serviceAlertsSeen {
		delete(d.seen, d.order[0])
		d.order = d.order[1:]
	}
	d.seen[key] = true
	d.order = append(d.order, key)
	return true
}

// SetCentralAlertSink sets what the service hands each alert from another
// board to: the first copy of each, from any network, verified and of a
// kind that network's sysop has on. It runs on a leaf's event goroutine
// and must not block. Nil drops them.
func (s *Service) SetCentralAlertSink(fn func(protocol.CentralAlert)) {
	s.alertSink.Store(alertSinkBox{fn})
}

type alertSinkBox struct{ fn func(protocol.CentralAlert) }

// receiveAlert is every leaf's OnAlert.
func (s *Service) receiveAlert(a protocol.CentralAlert) {
	box, _ := s.alertSink.Load().(alertSinkBox)
	if box.fn == nil || !s.alertsSeen.claim(a.NodeID, a.ID) {
		return
	}
	box.fn(a)
}

// SendCentralAlert sends spec, a caller's action the BBS has their
// agreement to announce, to network, or with network "" to every network,
// on each leaf whose sysop has spec's kind on and whose hub relays alerts.
// A fresh ID is set when spec has none, the same on every network. It
// returns how many leaves it was handed to; each sends in the background,
// retrying until the alert would be stale.
func (s *Service) SendCentralAlert(network string, spec leaf.AlertSpec) int {
	if spec.ID == "" {
		id, err := protocol.NewCentralAlertID()
		if err != nil {
			slog.Error("v3net: central alert id", "error", err)
			return 0
		}
		spec.ID = id
	}
	s.mu.RLock()
	var runs []*leafRun
	for _, run := range s.leaves {
		if network != "" && run.l.Network() != network {
			continue
		}
		if ok, _ := run.l.HubSupports(protocol.FeatureCentralAlerts); !ok || !run.l.AlertsEnabled(spec.Kind) || run.ctx == nil {
			continue
		}
		runs = append(runs, run)
	}
	s.mu.RUnlock()
	for _, run := range runs {
		go func(run *leafRun) {
			err := run.l.SendAlert(run.ctx, spec)
			switch {
			case err == nil, errors.Is(err, context.Canceled):
			case errors.Is(err, leaf.ErrAlertsOff), errors.Is(err, leaf.ErrAlertsUnsupported):
				slog.Debug("v3net: central alert not sent", "network", run.l.Network(), "kind", spec.Kind, "error", err)
			default:
				slog.Warn("v3net: central alert not sent", "network", run.l.Network(), "kind", spec.Kind, "error", err)
			}
		}(run)
	}
	return len(runs)
}

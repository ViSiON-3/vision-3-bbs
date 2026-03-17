package leaf

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// FetchNAL fetches and verifies the NAL for this leaf's network from the hub.
func (l *Leaf) FetchNAL(ctx context.Context) (*protocol.NAL, error) {
	url := l.cfg.HubURL + fmt.Sprintf("/v3net/v1/%s/nal", l.cfg.Network)
	n, err := nal.Fetch(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("leaf: fetch NAL: %w", err)
	}
	if err := nal.Verify(n); err != nil {
		return nil, fmt.Errorf("leaf: verify NAL: %w", err)
	}
	return n, nil
}

// Areas returns the list of areas from the cached NAL for this network.
// If no NAL is cached, it attempts to fetch one.
func (l *Leaf) Areas(ctx context.Context) ([]protocol.Area, error) {
	if l.nalCache == nil {
		return nil, fmt.Errorf("leaf: NAL cache not initialized")
	}

	n := l.nalCache.Get(l.cfg.Network)
	if n == nil {
		// Try to fetch.
		url := l.cfg.HubURL + fmt.Sprintf("/v3net/v1/%s/nal", l.cfg.Network)
		fetched, err := l.nalCache.FetchAndVerify(ctx, url, l.cfg.Network)
		if err != nil {
			return nil, fmt.Errorf("leaf: fetch NAL: %w", err)
		}
		return fetched.Areas, nil
	}
	return n.Areas, nil
}

// ProposeArea submits an area proposal to the hub and returns the response.
func (l *Leaf) ProposeArea(req protocol.AreaProposalRequest) (*protocol.ProposalResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("leaf: marshal proposal: %w", err)
	}
	path := fmt.Sprintf("/v3net/v1/%s/areas/propose", l.cfg.Network)
	resp, err := l.signedPostWithResponse(path, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pr protocol.ProposalResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, fmt.Errorf("leaf: decode proposal response: %w", err)
	}
	if resp.StatusCode != 200 {
		if pr.Error != "" {
			return nil, fmt.Errorf("hub: %s", pr.Error)
		}
		return nil, fmt.Errorf("leaf: propose returned %d", resp.StatusCode)
	}
	return &pr, nil
}

// handleNALUpdated schedules a NAL re-fetch after receiving an nal_updated SSE event.
func (l *Leaf) handleNALUpdated(ctx context.Context) {
	// Re-fetch within 60 seconds ±10% jitter.
	jitter := 1.0 + (rand.Float64()*2-1)*0.10
	delay := time.Duration(float64(60*time.Second) * jitter)

	slog.Info("leaf: NAL updated, re-fetching", "network", l.cfg.Network, "delay", delay)

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		url := l.cfg.HubURL + fmt.Sprintf("/v3net/v1/%s/nal", l.cfg.Network)
		if l.nalCache != nil {
			if _, err := l.nalCache.FetchAndVerify(ctx, url, l.cfg.Network); err != nil {
				slog.Warn("leaf: NAL re-fetch failed", "network", l.cfg.Network, "error", err)
			} else {
				slog.Info("leaf: NAL re-fetched", "network", l.cfg.Network)
			}
		}
	}()
}

// dispatchNALEvent handles NAL-related SSE events.
func (l *Leaf) dispatchNALEvent(ctx context.Context, ev protocol.Event) {
	switch ev.Type {
	case protocol.EventNALUpdated:
		l.handleNALUpdated(ctx)

	case protocol.EventAreaAccessRequested:
		var payload protocol.AreaAccessRequestedPayload
		if err := json.Unmarshal(ev.Data, &payload); err == nil {
			slog.Info("leaf: area access requested",
				"network", payload.Network, "tag", payload.Tag,
				"node", payload.NodeID, "bbs", payload.BBSName)
		}

	case protocol.EventProposalRejected:
		var payload protocol.ProposalRejectedPayload
		if err := json.Unmarshal(ev.Data, &payload); err == nil {
			slog.Warn("leaf: area proposal rejected",
				"network", payload.Network, "tag", payload.Tag, "reason", payload.Reason)
		}

	case protocol.EventSubscriptionDenied:
		var payload protocol.SubscriptionDeniedPayload
		if err := json.Unmarshal(ev.Data, &payload); err == nil {
			slog.Warn("leaf: subscription denied",
				"network", payload.Network, "tag", payload.Tag)
		}

	case protocol.EventCoordTransferPending:
		var payload protocol.CoordTransferPendingPayload
		if err := json.Unmarshal(ev.Data, &payload); err == nil {
			slog.Info("leaf: coordinator transfer pending",
				"network", payload.Network, "new_node", payload.NewNodeID)
		}
	}
}

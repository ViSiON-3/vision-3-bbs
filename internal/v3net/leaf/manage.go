package leaf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// The methods in this file drive the hub's management endpoints on behalf of
// a sysop: reviewing area proposals as the network coordinator, and reviewing
// area access requests as an area manager. The hub authorizes each call from
// the signed request's node ID, so a leaf that is neither coordinator nor
// manager gets a 403 and the hub's error message back.

// ListProposals returns the pending area proposals for this leaf's network.
// The hub answers only the network coordinator.
func (l *Leaf) ListProposals(ctx context.Context) ([]protocol.AreaProposal, error) {
	path := fmt.Sprintf("/v3net/v1/%s/areas/proposals", l.cfg.Network)
	body, err := l.signedCall(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var proposals []protocol.AreaProposal
	if err := json.Unmarshal(body, &proposals); err != nil {
		return nil, fmt.Errorf("leaf: decode proposals: %w", err)
	}
	return proposals, nil
}

// ApproveProposal approves a pending proposal, adding the area to the NAL.
// Overrides in req are optional; an empty access mode keeps the proposal's.
func (l *Leaf) ApproveProposal(ctx context.Context, proposalID string, req protocol.ProposalApproveRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("leaf: marshal approve: %w", err)
	}
	path := fmt.Sprintf("/v3net/v1/%s/areas/proposals/%s/approve", l.cfg.Network, proposalID)
	_, err = l.signedCall(ctx, http.MethodPost, path, data)
	return err
}

// RejectProposal rejects a pending proposal with an optional reason.
func (l *Leaf) RejectProposal(ctx context.Context, proposalID string, req protocol.ProposalRejectRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("leaf: marshal reject: %w", err)
	}
	path := fmt.Sprintf("/v3net/v1/%s/areas/proposals/%s/reject", l.cfg.Network, proposalID)
	_, err = l.signedCall(ctx, http.MethodPost, path, data)
	return err
}

// ListAccessRequests returns the pending subscription requests for one area.
// The hub answers only that area's manager.
func (l *Leaf) ListAccessRequests(ctx context.Context, tag string) ([]protocol.AccessRequest, error) {
	path := fmt.Sprintf("/v3net/v1/%s/areas/%s/access/requests", l.cfg.Network, tag)
	body, err := l.signedCall(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var reqs []protocol.AccessRequest
	if err := json.Unmarshal(body, &reqs); err != nil {
		return nil, fmt.Errorf("leaf: decode access requests: %w", err)
	}
	return reqs, nil
}

// ApproveAccess grants the listed nodes access to an area and activates their
// subscriptions.
func (l *Leaf) ApproveAccess(ctx context.Context, tag string, nodeIDs []string) error {
	return l.postNodeIDs(ctx, tag, "approve", protocol.NodeIDsRequest{NodeIDs: nodeIDs})
}

// DenyAccess refuses the listed nodes and puts them on the area's deny list.
func (l *Leaf) DenyAccess(ctx context.Context, tag string, nodeIDs []string, reason string) error {
	return l.postNodeIDs(ctx, tag, "deny", protocol.NodeIDsRequest{NodeIDs: nodeIDs, Reason: reason})
}

func (l *Leaf) postNodeIDs(ctx context.Context, tag, action string, req protocol.NodeIDsRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("leaf: marshal %s: %w", action, err)
	}
	path := fmt.Sprintf("/v3net/v1/%s/areas/%s/access/%s", l.cfg.Network, tag, action)
	_, err = l.signedCall(ctx, http.MethodPost, path, data)
	return err
}

// signedCall performs a signed GET or POST and returns the response body. A
// non-200 status becomes an error carrying the hub's own message when the
// body is the usual {"error": "..."} document.
func (l *Leaf) signedCall(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var (
		resp *http.Response
		err  error
	)
	switch method {
	case http.MethodGet:
		resp, err = l.signedGetCtx(ctx, path)
	case http.MethodPost:
		resp, err = l.signedPostWithResponse(ctx, path, body)
	default:
		return nil, fmt.Errorf("leaf: unsupported method %s", method)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() // read side

	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))
	if resp.StatusCode != http.StatusOK {
		var hubErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &hubErr) == nil && hubErr.Error != "" {
			return nil, fmt.Errorf("hub: %s", hubErr.Error)
		}
		return nil, fmt.Errorf("leaf: %s %s returned %d", method, path, resp.StatusCode)
	}
	if readErr != nil {
		return nil, fmt.Errorf("leaf: read %s response: %w", path, readErr)
	}
	return data, nil
}

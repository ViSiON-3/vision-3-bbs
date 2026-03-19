package hub

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

const proposalSchema = `
CREATE TABLE IF NOT EXISTS area_proposals (
	id          TEXT PRIMARY KEY,
	network     TEXT NOT NULL,
	tag         TEXT NOT NULL,
	name        TEXT NOT NULL,
	description TEXT,
	language    TEXT DEFAULT 'en',
	access_mode TEXT DEFAULT 'open',
	allow_ansi  INTEGER DEFAULT 1,
	from_node   TEXT NOT NULL,
	status      TEXT DEFAULT 'pending',
	reason      TEXT,
	proposed_at DATETIME DEFAULT (datetime('now')),
	resolved_at DATETIME
);
`

// ProposalStore handles SQLite-persisted area proposals.
type ProposalStore struct {
	db *sql.DB
}

// NewProposalStore initializes the proposals table.
func NewProposalStore(db *sql.DB) (*ProposalStore, error) {
	if _, err := db.Exec(proposalSchema); err != nil {
		return nil, fmt.Errorf("hub: create proposals table: %w", err)
	}
	return &ProposalStore{db: db}, nil
}

// newUUID generates a UUID v4 string.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("crypto/rand.Read: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// Add inserts a new proposal.
func (ps *ProposalStore) Add(network, tag, name, desc, lang, accessMode, fromNode string, allowANSI bool) (string, error) {
	id, err := newUUID()
	if err != nil {
		return "", fmt.Errorf("hub: generate proposal ID: %w", err)
	}
	_, err = ps.db.Exec(
		`INSERT INTO area_proposals (id, network, tag, name, description, language, access_mode, allow_ansi, from_node)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, network, tag, name, desc, lang, accessMode, boolToInt(allowANSI), fromNode,
	)
	if err != nil {
		return "", fmt.Errorf("hub: add proposal: %w", err)
	}
	return id, nil
}

// ListPending returns pending proposals for a network.
func (ps *ProposalStore) ListPending(network string) ([]protocol.AreaProposal, error) {
	rows, err := ps.db.Query(
		`SELECT p.id, p.tag, p.name, p.description, p.language, p.access_mode, p.allow_ansi, p.from_node,
		        COALESCE(s.bbs_name, ''), p.proposed_at, p.status
		 FROM area_proposals p
		 LEFT JOIN subscribers s ON p.from_node = s.node_id AND p.network = s.network
		 WHERE p.network = ? AND p.status = 'pending'
		 ORDER BY p.proposed_at ASC`, network,
	)
	if err != nil {
		return nil, fmt.Errorf("hub: list proposals: %w", err)
	}
	defer rows.Close()

	var result []protocol.AreaProposal
	for rows.Next() {
		var p protocol.AreaProposal
		var allowANSI int
		if err := rows.Scan(&p.ID, &p.Tag, &p.Name, &p.Description, &p.Language,
			&p.AccessMode, &allowANSI, &p.FromNode, &p.FromBBS, &p.ProposedAt, &p.Status); err != nil {
			return nil, fmt.Errorf("hub: scan proposal: %w", err)
		}
		p.AllowANSI = allowANSI != 0
		result = append(result, p)
	}
	return result, rows.Err()
}

// Get returns a single proposal by ID.
func (ps *ProposalStore) Get(id string) (*protocol.AreaProposal, error) {
	var p protocol.AreaProposal
	var allowANSI int
	var network, reason string
	err := ps.db.QueryRow(
		`SELECT id, network, tag, name, COALESCE(description, ''), language, access_mode, allow_ansi, from_node, status, COALESCE(reason, ''), proposed_at
		 FROM area_proposals WHERE id = ?`, id,
	).Scan(&p.ID, &network, &p.Tag, &p.Name, &p.Description, &p.Language,
		&p.AccessMode, &allowANSI, &p.FromNode, &p.Status, &reason, &p.ProposedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("hub: get proposal: %w", err)
	}
	p.AllowANSI = allowANSI != 0
	return &p, nil
}

// Resolve sets a proposal's status to approved or rejected.
func (ps *ProposalStore) Resolve(id, status, reason string) error {
	_, err := ps.db.Exec(
		`UPDATE area_proposals SET status = ?, reason = ?, resolved_at = datetime('now') WHERE id = ?`,
		status, reason, id,
	)
	if err != nil {
		return fmt.Errorf("hub: resolve proposal: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// handlePropose accepts a new area proposal from any authenticated subscriber.
func (h *Hub) handlePropose(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)

	var req protocol.AreaProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if err := protocol.ValidateAreaTag(req.Tag); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}

	lang := req.Language
	if lang == "" {
		lang = "en"
	}
	accessMode := req.AccessMode
	if accessMode == "" {
		accessMode = protocol.AccessModeOpen
	}
	if err := protocol.ValidateAccessMode(accessMode); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	id, err := h.proposals.Add(network, req.Tag, req.Name, req.Description, lang, accessMode, nodeID, req.AllowANSI)
	if err != nil {
		slog.Error("add proposal", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	status := "pending"

	// Auto-approve if hub is configured for it.
	if h.cfg.AutoApprove {
		if err := h.approveProposal(network, id, accessMode, req.AllowANSI); err != nil {
			slog.Error("auto-approve proposal", "id", id, "error", err)
			// Fall through — proposal is still stored as pending.
		} else {
			status = "approved"
			slog.Info("auto-approved area proposal", "network", network, "tag", req.Tag, "id", id)
		}
	}

	if status == "pending" {
		// Notify coordinator via SSE.
		ev, _ := protocol.NewEvent(protocol.EventAreaProposed, protocol.AreaProposedPayload{
			Network:    network,
			Tag:        req.Tag,
			FromNode:   nodeID,
			ProposalID: id,
		})
		h.broadcaster.Publish(network, ev)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"proposal_id": id,
		"status":      status,
	})
}

// handleListProposals returns pending proposals (coordinator-only).
func (h *Hub) handleListProposals(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)

	if !h.isCoordinator(network, nodeID) {
		http.Error(w, `{"error":"coordinator only"}`, http.StatusForbidden)
		return
	}

	proposals, err := h.proposals.ListPending(network)
	if err != nil {
		slog.Error("list proposals", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if proposals == nil {
		proposals = []protocol.AreaProposal{}
	}
	writeJSON(w, http.StatusOK, proposals)
}

// approveProposal is the core logic for approving a proposal: reads the
// proposal from the DB, adds the area to the NAL, signs, persists, and
// broadcasts the nal_updated event. It accepts optional overrides for
// access mode and allow_ansi (pass empty/false to use proposal defaults).
func (h *Hub) approveProposal(network, proposalID, accessModeOverride string, _ bool) error {
	// Serialize the entire approve operation to prevent concurrent approvals
	// from overwriting each other or appending duplicate areas.
	h.nalMu.Lock()
	defer h.nalMu.Unlock()

	// Read proposal from DB (inside lock to prevent TOCTOU with duplicate check).
	var tag, name, desc, lang, accessMode, fromNode string
	var allowANSI int
	err := h.proposals.db.QueryRow(
		`SELECT tag, name, description, language, access_mode, allow_ansi, from_node
		 FROM area_proposals WHERE id = ? AND network = ? AND status = 'pending'`,
		proposalID, network,
	).Scan(&tag, &name, &desc, &lang, &accessMode, &allowANSI, &fromNode)
	if err != nil {
		return fmt.Errorf("read proposal: %w", err)
	}

	if accessModeOverride != "" {
		accessMode = accessModeOverride
	}

	// Get manager pubkey.
	managerPubKeyB64 := ""
	managerSub := h.subscribers.Get(fromNode, network)
	if managerSub != nil {
		managerPubKeyB64 = managerSub.PubKeyB64
	}

	// Load current NAL (or create new one).
	currentNAL, err := h.nalStore.Get(network)
	if err != nil {
		return fmt.Errorf("get nal: %w", err)
	}
	if currentNAL == nil {
		currentNAL = &protocol.NAL{
			V3NetNAL: "1.0",
			Network:  network,
		}
	}

	// Check if tag already exists in the NAL (prevents duplicate appends).
	if currentNAL.FindArea(tag) != nil {
		// Area already in NAL — just resolve the proposal as approved.
		if err := h.proposals.Resolve(proposalID, "approved", "area already exists"); err != nil {
			slog.Error("resolve duplicate proposal", "error", err)
		}
		return nil
	}

	// Add the new area.
	newArea := protocol.Area{
		Tag:              tag,
		Name:             name,
		Description:      desc,
		Language:         lang,
		ManagerNodeID:    fromNode,
		ManagerPubKeyB64: managerPubKeyB64,
		Access: protocol.AreaAccess{
			Mode: accessMode,
		},
		Policy: protocol.AreaPolicy{
			MaxBodyBytes: protocol.MaxBodyBytes,
			AllowANSI:    allowANSI != 0,
		},
	}
	currentNAL.Areas = append(currentNAL.Areas, newArea)

	// Re-sign the NAL.
	if err := nal.Sign(currentNAL, h.cfg.Keystore); err != nil {
		return fmt.Errorf("sign nal: %w", err)
	}

	if err := h.nalStore.Put(network, currentNAL); err != nil {
		return fmt.Errorf("put nal: %w", err)
	}

	if err := h.proposals.Resolve(proposalID, "approved", ""); err != nil {
		slog.Error("resolve proposal", "error", err)
	}

	// Fan out nal_updated event.
	ev, _ := protocol.NewEvent(protocol.EventNALUpdated, protocol.NALUpdatedPayload{
		Network:   network,
		Updated:   currentNAL.Updated,
		AreaCount: len(currentNAL.Areas),
	})
	h.broadcaster.Publish(network, ev)

	return nil
}

// handleApproveProposal approves a proposal and adds the area to the NAL.
func (h *Hub) handleApproveProposal(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)
	proposalID := extractProposalID(r.URL.Path)

	if !h.isCoordinator(network, nodeID) {
		http.Error(w, `{"error":"coordinator only"}`, http.StatusForbidden)
		return
	}

	if proposalID == "" {
		http.Error(w, `{"error":"missing proposal ID"}`, http.StatusBadRequest)
		return
	}

	// Parse optional overrides (empty body is fine — overrides are optional).
	var overrides protocol.ProposalApproveRequest
	if err := json.NewDecoder(r.Body).Decode(&overrides); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if err := h.approveProposal(network, proposalID, overrides.AccessMode, false); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"proposal not found or not pending"}`, http.StatusNotFound)
			return
		}
		slog.Error("approve proposal", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleRejectProposal rejects a proposal.
func (h *Hub) handleRejectProposal(w http.ResponseWriter, r *http.Request) {
	network := extractNetwork(r.URL.Path)
	nodeID := r.Header.Get(headerNodeID)
	proposalID := extractProposalID(r.URL.Path)

	if !h.isCoordinator(network, nodeID) {
		http.Error(w, `{"error":"coordinator only"}`, http.StatusForbidden)
		return
	}

	if proposalID == "" {
		http.Error(w, `{"error":"missing proposal ID"}`, http.StatusBadRequest)
		return
	}

	// Read the proposal to get the tag and from_node.
	var tag, fromNode string
	err := h.proposals.db.QueryRow(
		`SELECT tag, from_node FROM area_proposals WHERE id = ? AND network = ? AND status = 'pending'`,
		proposalID, network,
	).Scan(&tag, &fromNode)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, `{"error":"proposal not found or not pending"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("get proposal for reject", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	var req protocol.ProposalRejectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if err := h.proposals.Resolve(proposalID, "rejected", req.Reason); err != nil {
		slog.Error("resolve proposal rejection", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Fan out proposal_rejected (NodeID allows the proposing node to identify it).
	ev, _ := protocol.NewEvent(protocol.EventProposalRejected, protocol.ProposalRejectedPayload{
		Network: network,
		Tag:     tag,
		Reason:  req.Reason,
		NodeID:  fromNode,
	})
	h.broadcaster.Publish(network, ev)

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// extractProposalID pulls the proposal ID from paths like:
// /v3net/v1/{network}/areas/proposals/{id}/approve
func extractProposalID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// v3net/v1/{network}/areas/proposals/{id}/approve|reject
	for i, p := range parts {
		if p == "proposals" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

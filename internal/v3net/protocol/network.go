package protocol

// NetworkInfo is the full metadata returned by GET /v3net/v1/{network}/info.
type NetworkInfo struct {
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	HubNodeID    string        `json:"hub_node_id"`
	HubPubKeyB64 string        `json:"hub_pubkey_b64,omitempty"`
	LeafCount    int           `json:"leaf_count"`
	MessageCount int64         `json:"message_count"`
	Policy       NetworkPolicy `json:"policy"`
}

// NetworkPolicy defines hub-enforced limits for a network.
type NetworkPolicy struct {
	MaxBodyBytes    int  `json:"max_body_bytes"`
	PollIntervalMin int  `json:"poll_interval_min"`
	RequireTearline bool `json:"require_tearline"`
}

// NetworkSummary is the short descriptor returned by GET /v3net/v1/networks.
type NetworkSummary struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	HubNodeID    string `json:"hub_node_id"`
	MessageCount int64  `json:"message_count"`
	CreatedAt    string `json:"created_at"`
}

// RegistryEntry is a network entry from the central V3Net registry.
type RegistryEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	HubURL      string `json:"hub_url"`
	HubNodeID   string `json:"hub_node_id"`
}

// Registry is the top-level structure of the V3Net registry JSON.
type Registry struct {
	V3NetRegistry string          `json:"v3net_registry"`
	Updated       string          `json:"updated"`
	Networks      []RegistryEntry `json:"networks"`
}

// SubscribeRequest is the body of POST /v3net/v1/subscribe.
type SubscribeRequest struct {
	Network   string   `json:"network"`
	NodeID    string   `json:"node_id"`
	PubKeyB64 string   `json:"pubkey_b64"`
	BBSName   string   `json:"bbs_name"`
	BBSHost   string   `json:"bbs_host"`
	AreaTags  []string `json:"area_tags,omitempty"`
}

// SubscribeResponse is the response to a subscribe request.
type SubscribeResponse struct {
	OK     bool   `json:"ok"`
	Status string `json:"status"`
}

// ChatRequest is the body of POST /v3net/v1/{network}/chat.
type ChatRequest struct {
	From string `json:"from"`
	Text string `json:"text"`
}

// PresenceRequest is the body of POST /v3net/v1/{network}/presence.
type PresenceRequest struct {
	Type   string `json:"type"` // "logon" or "logoff"
	Handle string `json:"handle"`
}

// MessageResponse is the response to a successful message POST.
type MessageResponse struct {
	OK      bool   `json:"ok"`
	MsgUUID string `json:"msg_uuid"`
}

// AreaProposal represents a proposed area awaiting coordinator approval.
type AreaProposal struct {
	ID          string `json:"id"`
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Language    string `json:"language"`
	AccessMode  string `json:"access_mode"`
	AllowANSI   bool   `json:"allow_ansi"`
	FromNode    string `json:"from_node"`
	FromBBS     string `json:"from_bbs"`
	ProposedAt  string `json:"proposed_at"`
	Status      string `json:"status"`
}

// ProposalResponse holds the hub's response to an area proposal.
type ProposalResponse struct {
	OK         bool   `json:"ok"`
	ProposalID string `json:"proposal_id"`
	Status     string `json:"status"`
	Error      string `json:"error"`
}

// AreaProposalRequest is the body of POST /areas/propose.
type AreaProposalRequest struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Language    string `json:"language"`
	AccessMode  string `json:"access_mode"`
	AllowANSI   bool   `json:"allow_ansi"`
}

// ProposalApproveRequest is the optional body for approve with overrides.
type ProposalApproveRequest struct {
	AccessMode    string `json:"access_mode,omitempty"`
	ManagerNodeID string `json:"manager_node_id,omitempty"`
}

// ProposalRejectRequest is the optional body for reject.
type ProposalRejectRequest struct {
	Reason string `json:"reason"`
}

// AccessRequest represents a pending area subscription request.
type AccessRequest struct {
	NodeID      string `json:"node_id"`
	BBSName     string `json:"bbs_name"`
	BBSHost     string `json:"bbs_host"`
	RequestedAt string `json:"requested_at"`
}

// AreaAccessConfig is the access configuration for an area.
type AreaAccessConfig struct {
	Mode      string   `json:"mode"`
	AllowList []string `json:"allow_list"`
	DenyList  []string `json:"deny_list"`
}

// AccessModeRequest is the body of POST /areas/{tag}/access/mode.
type AccessModeRequest struct {
	Mode string `json:"mode"`
}

// NodeIDsRequest is the body for approve/deny/remove endpoints.
type NodeIDsRequest struct {
	NodeIDs []string `json:"node_ids"`
	Reason  string   `json:"reason,omitempty"`
}

// AreaSubscriptionStatus is a per-area status in the subscribe response.
type AreaSubscriptionStatus struct {
	Tag    string `json:"tag"`
	Status string `json:"status"`
}

// SubscribeWithAreasResponse is the response when area_tags are provided.
// Status is the network-level subscription status (e.g. "active", "pending").
// Areas contains per-area subscription statuses; may be empty if no areas were processed.
type SubscribeWithAreasResponse struct {
	OK     bool                     `json:"ok"`
	Status string                   `json:"status"`
	Areas  []AreaSubscriptionStatus `json:"areas,omitempty"`
}

// CoordTransferRequest is the body of POST /coordinator/transfer.
type CoordTransferRequest struct {
	NewNodeID    string `json:"new_node_id"`
	NewPubKeyB64 string `json:"new_pubkey_b64"`
}

// CoordAcceptRequest is the body of POST /coordinator/accept.
type CoordAcceptRequest struct {
	Token string `json:"token"`
}

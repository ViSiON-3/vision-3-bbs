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
	Name        string   `json:"name"`
	Description string   `json:"description"`
	HubURL      string   `json:"hub_url"`
	HubNodeID   string   `json:"hub_node_id"`
	Tags        []string `json:"tags"`
}

// Registry is the top-level structure of the V3Net registry JSON.
type Registry struct {
	V3NetRegistry string          `json:"v3net_registry"`
	Updated       string          `json:"updated"`
	Networks      []RegistryEntry `json:"networks"`
}

// SubscribeRequest is the body of POST /v3net/v1/subscribe.
type SubscribeRequest struct {
	Network   string `json:"network"`
	NodeID    string `json:"node_id"`
	PubKeyB64 string `json:"pubkey_b64"`
	BBSName   string `json:"bbs_name"`
	BBSHost   string `json:"bbs_host"`
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
	Type   string `json:"type"`   // "logon" or "logoff"
	Handle string `json:"handle"`
}

// MessageResponse is the response to a successful message POST.
type MessageResponse struct {
	OK      bool   `json:"ok"`
	MsgUUID string `json:"msg_uuid"`
}

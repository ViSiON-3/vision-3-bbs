package protocol

import "encoding/json"

// SSE event type constants.
const (
	EventPing       = "ping"
	EventLogon      = "logon"
	EventLogoff     = "logoff"
	EventNewMessage = "new_message"
	EventChat       = "chat"

	// NAL-related event types (Phase 13).
	EventNALUpdated           = "nal_updated"
	EventAreaProposed         = "area_proposed"
	EventAreaAccessRequested  = "area_access_requested"
	EventProposalRejected     = "proposal_rejected"
	EventSubscriptionDenied   = "subscription_denied"
	EventCoordTransferPending = "coordinator_transfer_pending"
)

// Event represents a Server-Sent Event on the V3Net event stream.
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// LogonPayload is the data for a logon event.
type LogonPayload struct {
	Handle    string `json:"handle"`
	Node      string `json:"node"`
	Timestamp string `json:"timestamp"`
}

// LogoffPayload is the data for a logoff event.
type LogoffPayload struct {
	Handle    string `json:"handle"`
	Node      string `json:"node"`
	Timestamp string `json:"timestamp"`
}

// NewMessagePayload is the data for a new_message event.
type NewMessagePayload struct {
	Network string `json:"network"`
	MsgUUID string `json:"msg_uuid"`
	From    string `json:"from"`
	Subject string `json:"subject"`
}

// ChatPayload is the data for a chat event.
type ChatPayload struct {
	From      string `json:"from"`
	Node      string `json:"node"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
}

// PingPayload is the data for a ping keepalive event.
type PingPayload struct{}

// NALUpdatedPayload notifies leaf nodes that the NAL has been updated.
type NALUpdatedPayload struct {
	Network   string `json:"network"`
	Updated   string `json:"updated"`
	AreaCount int    `json:"area_count"`
}

// AreaProposedPayload notifies the coordinator of a new area proposal.
type AreaProposedPayload struct {
	Network    string `json:"network"`
	Tag        string `json:"tag"`
	FromNode   string `json:"from_node"`
	ProposalID string `json:"proposal_id"`
}

// AreaAccessRequestedPayload notifies an area manager of a pending subscription.
type AreaAccessRequestedPayload struct {
	Network string `json:"network"`
	Tag     string `json:"tag"`
	NodeID  string `json:"node_id"`
	BBSName string `json:"bbs_name"`
}

// ProposalRejectedPayload notifies the proposing node of rejection.
// NodeID identifies the target node so other leaves can ignore the event.
type ProposalRejectedPayload struct {
	Network string `json:"network"`
	Tag     string `json:"tag"`
	Reason  string `json:"reason"`
	NodeID  string `json:"node_id"`
}

// SubscriptionDeniedPayload notifies a node that area access was denied.
// NodeID identifies the target node so other leaves can ignore the event.
type SubscriptionDeniedPayload struct {
	Network string `json:"network"`
	Tag     string `json:"tag"`
	NodeID  string `json:"node_id"`
}

// CoordTransferPendingPayload notifies the incoming coordinator.
type CoordTransferPendingPayload struct {
	Network   string `json:"network"`
	NewNodeID string `json:"new_node_id"`
}

// NewEvent creates an Event by marshaling the given payload.
func NewEvent(eventType string, payload any) (Event, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	return Event{Type: eventType, Data: data}, nil
}

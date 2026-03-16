package protocol

import "encoding/json"

// SSE event type constants.
const (
	EventPing       = "ping"
	EventLogon      = "logon"
	EventLogoff     = "logoff"
	EventNewMessage = "new_message"
	EventChat       = "chat"
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

// NewEvent creates an Event by marshaling the given payload.
func NewEvent(eventType string, payload any) (Event, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	return Event{Type: eventType, Data: data}, nil
}

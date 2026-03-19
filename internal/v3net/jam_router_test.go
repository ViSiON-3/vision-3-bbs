package v3net

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

type mockWriter struct {
	messages []protocol.Message
}

func (m *mockWriter) WriteMessage(msg protocol.Message) (int64, error) {
	m.messages = append(m.messages, msg)
	return int64(len(m.messages)), nil
}

func TestJAMRouter_DispatchesToCorrectAdapter(t *testing.T) {
	general := &mockWriter{}
	coding := &mockWriter{}
	router := NewJAMRouter()
	router.Add("gen.general", general)
	router.Add("gen.coding", coding)

	msg := protocol.Message{AreaTag: "gen.general", MsgUUID: "uuid-1"}
	if _, err := router.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	if len(general.messages) != 1 {
		t.Errorf("expected 1 message to general, got %d", len(general.messages))
	}
	if len(coding.messages) != 0 {
		t.Errorf("expected 0 messages to coding, got %d", len(coding.messages))
	}
}

func TestJAMRouter_UnknownAreaTagReturnsZero(t *testing.T) {
	router := NewJAMRouter()

	msg := protocol.Message{AreaTag: "gen.unknown", MsgUUID: "uuid-1"}
	num, err := router.WriteMessage(msg)
	if err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if num != 0 {
		t.Errorf("expected 0 for unknown area, got %d", num)
	}
}

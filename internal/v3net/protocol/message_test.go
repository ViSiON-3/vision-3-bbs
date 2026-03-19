package protocol

import (
	"strings"
	"testing"
)

func validMessage() Message {
	return Message{
		V3Net:       "1.0",
		Network:     "felonynet",
		AreaTag:     "fel.general",
		MsgUUID:     "550e8400-e29b-41d4-a716-446655440000",
		ThreadUUID:  "550e8400-e29b-41d4-a716-446655440000",
		ParentUUID:  nil,
		OriginNode:  "bbs.example.net",
		OriginBoard: "General",
		From:        "Darkstar",
		To:          "All",
		Subject:     "Hello from the underground",
		DateUTC:     "2026-03-16T04:20:00Z",
		Body:        "This is a test message.",
		Kludges:     map[string]any{},
	}
}

func strPtr(s string) *string { return &s }

func TestValidate_ValidMessage(t *testing.T) {
	m := validMessage()
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid message, got error: %v", err)
	}
}

func TestValidate_ValidWithParentUUID(t *testing.T) {
	m := validMessage()
	m.ParentUUID = strPtr("660e8400-e29b-41d4-a716-446655440000")
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid message with parent_uuid, got error: %v", err)
	}
}

func TestValidate_InvalidFields(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Message)
		wantErr string
	}{
		{
			name:    "bad protocol version",
			modify:  func(m *Message) { m.V3Net = "2.0" },
			wantErr: "unsupported protocol version",
		},
		{
			name:    "empty protocol version",
			modify:  func(m *Message) { m.V3Net = "" },
			wantErr: "unsupported protocol version",
		},
		{
			name:    "invalid network uppercase",
			modify:  func(m *Message) { m.Network = "FelonyNet" },
			wantErr: "invalid network name",
		},
		{
			name:    "empty network",
			modify:  func(m *Message) { m.Network = "" },
			wantErr: "invalid network name",
		},
		{
			name:    "network too long",
			modify:  func(m *Message) { m.Network = strings.Repeat("a", 33) },
			wantErr: "invalid network name",
		},
		{
			name:    "network with spaces",
			modify:  func(m *Message) { m.Network = "my net" },
			wantErr: "invalid network name",
		},
		{
			name:    "empty area_tag",
			modify:  func(m *Message) { m.AreaTag = "" },
			wantErr: "invalid area_tag",
		},
		{
			name:    "invalid area_tag format",
			modify:  func(m *Message) { m.AreaTag = "INVALID" },
			wantErr: "invalid area_tag",
		},
		{
			name:    "invalid msg_uuid",
			modify:  func(m *Message) { m.MsgUUID = "not-a-uuid" },
			wantErr: "invalid msg_uuid",
		},
		{
			name:    "msg_uuid wrong version",
			modify:  func(m *Message) { m.MsgUUID = "550e8400-e29b-51d4-a716-446655440000" },
			wantErr: "invalid msg_uuid",
		},
		{
			name:    "invalid thread_uuid",
			modify:  func(m *Message) { m.ThreadUUID = "bad" },
			wantErr: "invalid thread_uuid",
		},
		{
			name:    "invalid parent_uuid",
			modify:  func(m *Message) { m.ParentUUID = strPtr("bad") },
			wantErr: "invalid parent_uuid",
		},
		{
			name:    "invalid date_utc",
			modify:  func(m *Message) { m.DateUTC = "not-a-date" },
			wantErr: "invalid date_utc",
		},
		{
			name:    "date_utc wrong format",
			modify:  func(m *Message) { m.DateUTC = "2026-03-16 04:20:00" },
			wantErr: "invalid date_utc",
		},
		{
			name:    "empty from",
			modify:  func(m *Message) { m.From = "" },
			wantErr: "from must be 1",
		},
		{
			name:    "from too long",
			modify:  func(m *Message) { m.From = strings.Repeat("A", 65) },
			wantErr: "from must be 1",
		},
		{
			name:    "from non-ascii",
			modify:  func(m *Message) { m.From = "Dàrkstar" },
			wantErr: "non-printable or non-ASCII",
		},
		{
			name:    "from control character",
			modify:  func(m *Message) { m.From = "Dark\x00star" },
			wantErr: "non-printable or non-ASCII",
		},
		{
			name:    "empty to",
			modify:  func(m *Message) { m.To = "" },
			wantErr: "to must be 1",
		},
		{
			name:    "empty subject",
			modify:  func(m *Message) { m.Subject = "" },
			wantErr: "subject must be 1",
		},
		{
			name:    "subject too long",
			modify:  func(m *Message) { m.Subject = strings.Repeat("A", 129) },
			wantErr: "subject must be 1",
		},
		{
			name:    "empty body",
			modify:  func(m *Message) { m.Body = "" },
			wantErr: "body must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validMessage()
			tt.modify(&m)
			err := m.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	m := validMessage()
	m.Body = strings.Repeat("x", MaxBodyBytes+100)

	if !m.NeedsTruncation() {
		t.Fatal("expected NeedsTruncation to be true")
	}

	if m.IsTruncated() {
		t.Fatal("expected IsTruncated to be false before Truncate")
	}

	m.Truncate()

	if len(m.Body) != MaxBodyBytes {
		t.Errorf("expected body length %d, got %d", MaxBodyBytes, len(m.Body))
	}
	if !m.IsTruncated() {
		t.Error("expected IsTruncated to be true after Truncate")
	}
	if m.Kludges["v3net_truncated"] != true {
		t.Error("expected v3net_truncated kludge to be true")
	}
}

func TestTruncate_NoOp(t *testing.T) {
	m := validMessage()
	m.Truncate()

	if m.Kludges["v3net_truncated"] != nil {
		t.Error("expected no truncation kludge on short message")
	}
}

func TestValidate_NetworkEdgeCases(t *testing.T) {
	valid := []string{"a", "my-net", "net_123", strings.Repeat("a", 32)}
	for _, n := range valid {
		m := validMessage()
		m.Network = n
		if err := m.Validate(); err != nil {
			t.Errorf("expected network %q to be valid, got: %v", n, err)
		}
	}
}

package message

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitNetmailTo(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantAddr string
	}{
		{"AreaFix", "AreaFix", ""},
		{"All", "All", ""},
		{"areafix@3:633/2744", "areafix", "3:633/2744"},
		{"AreaFix@21:1/100", "AreaFix", "21:1/100"},
		{"SysOp@3:633/2744.11", "SysOp", "3:633/2744.11"},
		{"user@example.com", "user@example.com", ""}, // email-like, not FTN
		{"@3:633/2744", "@3:633/2744", ""},           // no username before @
		{"J0hn Doe@1:2/3", "J0hn Doe", "1:2/3"},      // space in name
		{"", "", ""},
	}

	for _, tt := range tests {
		name, addr := splitNetmailTo(tt.input)
		if name != tt.wantName || addr != tt.wantAddr {
			t.Errorf("splitNetmailTo(%q) = (%q, %q), want (%q, %q)",
				tt.input, name, addr, tt.wantName, tt.wantAddr)
		}
	}
}

// newNetmailTestManager builds a manager with a single netmail area addressed
// as 21:4/158.
func newNetmailTestManager(t *testing.T) *MessageManager {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	areas := `[{"id":1,"tag":"NETMAIL","name":"Netmail","base_path":"netmail",
	            "area_type":"netmail","origin_addr":"21:4/158","network":"fsxnet"}]`
	if err := os.WriteFile(filepath.Join(cfg, "message_areas.json"), []byte(areas), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, err := NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	return mm
}

// A netmail reply has to carry the addressee's FTN address: without a
// DestAddr the JAM message has no DADDRESS subfield and the tosser cannot
// address the outbound packet. The sender's point must survive too, since the
// tosser writes it as the TOPT control paragraph.
func TestAddPrivateReply_NetmailKeepsDestAddr(t *testing.T) {
	mm := newNetmailTestManager(t)

	parent, err := mm.AddPrivateMessage(1, "Bob", "Alice@21:1/100.5", "Hello", "hi", "")
	if err != nil {
		t.Fatalf("AddPrivateMessage: %v", err)
	}

	num, err := mm.AddPrivateReply(1, "Alice", "Bob@21:1/100.5", "Re: Hello", "hi back", "", parent)
	if err != nil {
		t.Fatalf("AddPrivateReply: %v", err)
	}

	reply, err := mm.GetMessage(1, num)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if reply.To != "Bob" {
		t.Errorf("To = %q, want %q", reply.To, "Bob")
	}
	if reply.DestAddr != "21:1/100.5" {
		t.Errorf("DestAddr = %q, want %q", reply.DestAddr, "21:1/100.5")
	}
	if reply.OrigAddr != "21:4/158" {
		t.Errorf("OrigAddr = %q, want %q", reply.OrigAddr, "21:4/158")
	}
	if reply.ReplyToNum != parent {
		t.Errorf("ReplyToNum = %d, want %d", reply.ReplyToNum, parent)
	}
}

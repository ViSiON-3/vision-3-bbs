package jam

import "testing"

func TestCountMessagesToUser(t *testing.T) {
	b := openTestBase(t)

	recipients := []string{"All", "SysOp", "all", "sysop", "Someone Else"}
	for _, to := range recipients {
		msg := NewMessage()
		msg.From = "Tester"
		msg.To = to
		msg.Subject = "Msg"
		msg.Text = "Body"
		if _, err := b.WriteMessage(msg); err != nil {
			t.Fatalf("WriteMessage to %q: %v", to, err)
		}
	}

	tests := []struct {
		username string
		want     int
	}{
		{"SysOp", 2}, // matching is case-insensitive
		{"sysop", 2},
		{"All", 2},
		{"Someone Else", 1},
		{"Nobody", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got, err := b.CountMessagesToUser(tt.username)
		if err != nil {
			t.Fatalf("CountMessagesToUser(%q): %v", tt.username, err)
		}
		if got != tt.want {
			t.Errorf("CountMessagesToUser(%q) = %d, want %d", tt.username, got, tt.want)
		}
	}
}

func TestCountMessagesToUserEmptyBase(t *testing.T) {
	b := openTestBase(t)

	got, err := b.CountMessagesToUser("SysOp")
	if err != nil {
		t.Fatalf("CountMessagesToUser: %v", err)
	}
	if got != 0 {
		t.Errorf("empty base: got %d, want 0", got)
	}
}

// TestCountMessagesToUserAcrossChunks covers a base larger than one .jdx read
// batch, so a mistake in the chunk arithmetic shows up as a wrong tally.
func TestCountMessagesToUserAcrossChunks(t *testing.T) {
	orig := indexScanChunk
	indexScanChunk = 4
	t.Cleanup(func() { indexScanChunk = orig })

	b := openTestBase(t)

	const total = 11 // spans two full batches plus a partial one
	want := 0
	for i := 0; i < total; i++ {
		msg := NewMessage()
		msg.From = "Tester"
		msg.To = "All"
		if i%3 == 0 {
			msg.To = "SysOp"
			want++
		}
		msg.Subject = "Msg"
		msg.Text = "Body"
		if _, err := b.WriteMessage(msg); err != nil {
			t.Fatalf("WriteMessage %d: %v", i, err)
		}
	}

	got, err := b.CountMessagesToUser("SysOp")
	if err != nil {
		t.Fatalf("CountMessagesToUser: %v", err)
	}
	if got != want {
		t.Errorf("CountMessagesToUser = %d, want %d", got, want)
	}
}

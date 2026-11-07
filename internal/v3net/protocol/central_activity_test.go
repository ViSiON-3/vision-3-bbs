package protocol_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// activityKS returns a fresh keystore.
func activityKS(t *testing.T) *keystore.Keystore {
	t.Helper()
	ks, _, err := keystore.Load(filepath.Join(t.TempDir(), "k.key"))
	if err != nil {
		t.Fatal(err)
	}
	return ks
}

// presenceSnapshot returns an unsigned presence snapshot on testnet
// published at now, valid for three minutes.
func presenceSnapshot(now time.Time, entries ...protocol.CentralActivityEntry) *protocol.CentralActivity {
	return &protocol.CentralActivity{
		V3NetCentral: protocol.CentralActivityVersion, Kind: protocol.CentralKindPresence, Network: "testnet", NodeID: "aaaa1111bbbb2222",
		Seq: now.UnixNano(), BBS: "Alpha", Entries: entries,
		PublishedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(3 * time.Minute).Format(time.RFC3339),
	}
}

// A signed snapshot verifies, and any change to what it covers breaks it.
func TestCentralActivitySignVerify(t *testing.T) {
	ks := activityKS(t)
	now := time.Now().UTC().Truncate(time.Second)
	a := presenceSnapshot(now, protocol.CentralActivityEntry{Handle: "Alice", Session: "abcdefghijklmnop", Availability: protocol.CentralAvailable})
	if err := a.Sign(ks); err != nil {
		t.Fatal(err)
	}
	if err := a.Validate("testnet", protocol.CentralKindPresence); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := a.Verify(); err != nil {
		t.Fatalf("verify: %v", err)
	}
	// Any change to what was signed breaks it: a handle added by a relay,
	// or a snapshot passed off as another kind.
	a.Entries = append(a.Entries, protocol.CentralActivityEntry{Handle: "Mallory", Session: "qrstuvwxyzABCDEF"})
	if err := a.Verify(); err == nil {
		t.Error("an added entry still verifies")
	}
	a.Entries = a.Entries[:1]
	a.Kind = protocol.CentralKindLastCallers
	if err := a.Verify(); err == nil {
		t.Error("a changed kind still verifies")
	}
	a.Kind = protocol.CentralKindPresence
	if err := a.Verify(); err != nil {
		t.Errorf("restored snapshot: %v", err)
	}
	if err := (&protocol.CentralActivity{}).Verify(); err != protocol.ErrCentralActivityUnsigned {
		t.Errorf("unsigned: %v", err)
	}
}

// Validate enforces each kind's form and limits, retention included.
func TestCentralActivityValidate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	calls := func(entries ...protocol.CentralActivityEntry) *protocol.CentralActivity {
		a := presenceSnapshot(now, entries...)
		a.Kind = protocol.CentralKindLastCallers
		a.ExpiresAt = at(time.Hour)
		return a
	}
	ok := protocol.CentralActivityEntry{Handle: "Alice", Session: "abcdefghijklmnop"}
	many := make([]protocol.CentralActivityEntry, protocol.MaxCentralPresenceEntries+1)
	for i := range many {
		many[i] = protocol.CentralActivityEntry{Handle: "h", Session: strings.Repeat("a", 10) + string(rune('A'+i%26)) + string(rune('a'+i/26))}
	}
	for name, tc := range map[string]struct {
		a    *protocol.CentralActivity
		kind string
		ok   bool
	}{
		"presence":            {presenceSnapshot(now, ok), protocol.CentralKindPresence, true},
		"no entries":          {presenceSnapshot(now), protocol.CentralKindPresence, true},
		"wrong kind":          {presenceSnapshot(now, ok), protocol.CentralKindLastCallers, false},
		"pipe in handle":      {presenceSnapshot(now, protocol.CentralActivityEntry{Handle: "|12Al", Session: "abcdefghijklmnop"}), protocol.CentralKindPresence, false},
		"long handle":         {presenceSnapshot(now, protocol.CentralActivityEntry{Handle: strings.Repeat("x", 31), Session: "abcdefghijklmnop"}), protocol.CentralKindPresence, false},
		"bad availability":    {presenceSnapshot(now, protocol.CentralActivityEntry{Handle: "A", Session: "abcdefghijklmnop", Availability: "lunch"}), protocol.CentralKindPresence, false},
		"no session":          {presenceSnapshot(now, protocol.CentralActivityEntry{Handle: "A"}), protocol.CentralKindPresence, false},
		"duplicate session":   {presenceSnapshot(now, ok, ok), protocol.CentralKindPresence, false},
		"presence with times": {presenceSnapshot(now, protocol.CentralActivityEntry{Handle: "A", Session: "abcdefghijklmnop", At: at(0)}), protocol.CentralKindPresence, false},
		"too many sessions":   {presenceSnapshot(now, many...), protocol.CentralKindPresence, false},
		"presence lives long": {func() *protocol.CentralActivity {
			a := presenceSnapshot(now)
			a.ExpiresAt = at(6 * time.Minute)
			return a
		}(), protocol.CentralKindPresence, false},
		"call":              {calls(protocol.CentralActivityEntry{Handle: "A", At: at(-2 * time.Hour), Until: at(-time.Hour)}), protocol.CentralKindLastCallers, true},
		"call without at":   {calls(protocol.CentralActivityEntry{Handle: "A"}), protocol.CentralKindLastCallers, false},
		"call too old":      {calls(protocol.CentralActivityEntry{Handle: "A", At: at(-25 * time.Hour)}), protocol.CentralKindLastCallers, false},
		"call ends first":   {calls(protocol.CentralActivityEntry{Handle: "A", At: at(-time.Hour), Until: at(-2 * time.Hour)}), protocol.CentralKindLastCallers, false},
		"call in future":    {calls(protocol.CentralActivityEntry{Handle: "A", At: at(time.Hour)}), protocol.CentralKindLastCallers, false},
		"call with session": {calls(protocol.CentralActivityEntry{Handle: "A", At: at(-time.Hour), Session: "abcdefghijklmnop"}), protocol.CentralKindLastCallers, false},
		// A snapshot may not outlive its oldest call's retention: a board
		// that goes offline cannot leave a call up past 24 hours.
		"outlives oldest call": {func() *protocol.CentralActivity {
			a := calls(protocol.CentralActivityEntry{Handle: "A", At: at(-23 * time.Hour)})
			a.ExpiresAt = at(2 * time.Hour)
			return a
		}(), protocol.CentralKindLastCallers, false},
	} {
		err := tc.a.Validate("testnet", tc.kind)
		if (err == nil) != tc.ok {
			t.Errorf("%s: ok=%v, err=%v", name, tc.ok, err)
		}
	}
}

// CleanCentralText makes text safe for a snapshot and cuts it to length.
func TestCleanCentralText(t *testing.T) {
	if got := protocol.CleanCentralText(" Al|ce\x07 ", 30); got != "Al/ce" {
		t.Errorf("got %q", got)
	}
	if got := protocol.CleanCentralText(strings.Repeat("é", 40), 30); len([]rune(got)) != 30 {
		t.Errorf("not cut to 30: %q", got)
	}
}

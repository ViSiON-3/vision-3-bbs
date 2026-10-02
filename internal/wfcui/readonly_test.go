package wfcui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A daemon that marks the account read-only disables the same keys as
// --readonly, with no client flag set.
func TestServerReadOnlyDisablesNodeKeys(t *testing.T) {
	sc := &snoopClient{fakeClient: newFakeClient()}
	m, _ := newTestModel(sc, Options{NoBell: true})
	m = withNodes(m)
	m.snapshot.ReadOnly = true

	m, _ = update(t, m, keyRune('k'))
	if m.mode != modeList || !strings.Contains(m.status, "Read-only") {
		t.Fatalf("K: mode=%v status=%q", m.mode, m.status)
	}
	m, cmd := update(t, m, keyRune('s'))
	if cmd != nil || !strings.Contains(m.status, "Read-only") {
		t.Fatalf("S: status %q", m.status)
	}

	m, _ = feed(t, m, pageEvent(1, "help"))
	m, _ = update(t, m, keyRune('p'))
	if m.mode != modePages {
		t.Fatalf("P: mode %v", m.mode)
	}
	if strings.Contains(m.View(), "answer") {
		t.Error("read-only pages list advertises answer")
	}
	m, cmd = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || !strings.Contains(m.status, "Read-only") {
		t.Fatalf("Enter: status %q", m.status)
	}
	if len(sc.execs) != 0 {
		t.Fatalf("commands sent: %v", sc.execs)
	}
}

func TestReadOnlyShownInTitle(t *testing.T) {
	m := makeModel(Options{}, 100, 30)
	m.snapshot = mockupSnapshot(m.now())
	if strings.Contains(strings.ToLower(rows(m.View())[0]), "read-only") {
		t.Fatal("title says read-only for a normal console")
	}
	m.snapshot.ReadOnly = true
	if strings.Contains(m.View(), "kick") {
		t.Error("server read-only console advertises kick")
	}
	if !strings.Contains(strings.ToLower(rows(m.View())[0]), "read-only") {
		t.Errorf("server read-only not in title: %q", rows(m.View())[0])
	}

	m = makeModel(Options{ReadOnly: true}, 100, 30)
	m.snapshot = mockupSnapshot(m.now())
	if !strings.Contains(strings.ToLower(rows(m.View())[0]), "read-only") {
		t.Errorf("--readonly not in title: %q", rows(m.View())[0])
	}
}

package configeditor

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestHandleNodelistMsgURLMismatchDropped verifies that a result whose url
// doesn't match the wizard's current nodelistURL is dropped without
// mutating any state — this guards against a late result from network A's
// URL being applied after the wizard has moved on to network B.
func TestHandleNodelistMsgURLMismatchDropped(t *testing.T) {
	m := newLookupWizardModel("https://example.org/new.zip")
	m.ftnWizard.ownAddress = "21:4/158"
	m.ftnWizard.lookupLoading = true
	m.mode = modeFTNNodelistLookup

	res, cmd := m.handleFTNNodelistMsg(ftnNodelistMsg{
		url:      "https://example.org/old.zip",
		nodelist: lookupTestNodelist(t),
	})
	m2 := res.(Model)

	if cmd != nil {
		t.Error("mismatched url result must not produce a follow-up cmd")
	}
	if m2.mode != modeFTNNodelistLookup {
		t.Errorf("mode = %v, want unchanged modeFTNNodelistLookup", m2.mode)
	}
	if !m2.ftnWizard.lookupLoading {
		t.Error("lookupLoading must remain true — the real fetch for the current URL is still pending")
	}
	if m2.ftnWizard.nodelist != nil {
		t.Error("stale result must not populate the nodelist cache")
	}
	if m2.ftnWizard.hubAddress != "" || m2.ftnWizard.hubHostname != "" {
		t.Error("stale result must not autofill hub fields")
	}
}

// TestEscCancelsInFlightFetch verifies ESC invokes the stored cancel func
// and clears it, so the underlying HTTP request is actually torn down
// instead of merely being hidden from the UI.
func TestEscCancelsInFlightFetch(t *testing.T) {
	m := newLookupWizardModel("https://example.org/nl.zip")
	m.mode = modeFTNNodelistLookup
	m.ftnWizard.lookupLoading = true

	called := false
	m.ftnWizard.lookupCancel = func() { called = true }

	res, _ := m.updateFTNNodelistLookup(tea.KeyMsg{Type: tea.KeyEscape})
	m2 := res.(Model)

	if !called {
		t.Error("ESC must invoke the stored lookupCancel func")
	}
	if m2.ftnWizard.lookupCancel != nil {
		t.Error("lookupCancel must be nil'd out after ESC")
	}
	if m2.mode != modeFTNWizardForm {
		t.Errorf("mode = %v, want modeFTNWizardForm", m2.mode)
	}
}

// TestStaleGenerationSameURLDropped verifies that a late result from a
// cancelled-then-retried lookup against the *same* URL is dropped. The URL
// guard alone can't catch this case (cancel then immediately retry the same
// network reuses the same URL) — only the generation counter, bumped on
// every new startFTNNodeLookup call, can tell the late first attempt's
// result apart from the second attempt's real result.
func TestStaleGenerationSameURLDropped(t *testing.T) {
	m := newLookupWizardModel("https://example.org/nl.zip")
	m.ftnWizard.ownAddress = "21:4/158"
	m.ftnWizard.lookupLoading = true
	m.ftnWizard.lookupGeneration = 2
	m.mode = modeFTNNodelistLookup

	res, cmd := m.handleFTNNodelistMsg(ftnNodelistMsg{
		url:        "https://example.org/nl.zip",
		generation: 1,
		err:        context.Canceled,
	})
	m2 := res.(Model)

	if cmd != nil {
		t.Error("stale generation result must not produce a follow-up cmd")
	}
	if m2.mode != modeFTNNodelistLookup {
		t.Errorf("mode = %v, want unchanged modeFTNNodelistLookup", m2.mode)
	}
	if !m2.ftnWizard.lookupLoading {
		t.Error("lookupLoading must remain true — the real fetch for the current generation is still pending")
	}
	if m2.ftnWizard.lookupErr != "" {
		t.Errorf("lookupErr = %q, want empty — the stale context-canceled error must not surface", m2.ftnWizard.lookupErr)
	}
	if m2.message != "" {
		t.Errorf("message = %q, want empty — stale result must not surface as a user-facing message", m2.message)
	}
}

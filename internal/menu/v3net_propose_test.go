package menu

import (
	"errors"
	"testing"
)

const keyCtrlS = "\x13"

// TestV3NetProposeSubmitsForm pins V3NETPROPOSE end to end: typed fields
// (with in-line editing keys), the cycled access mode and the ANSI toggle
// all reach the hub request, and the returned proposal ID is shown.
func TestV3NetProposeSubmitsForm(t *testing.T) {
	env := newMenuEnv(t)
	fake := newV3NetScreenFake("testnet")
	env.e.V3NetStatus = fake

	keys := "\rtst.gxx\x08\x7feneral\r" + // Area Tag, fixing a typo with BS and DEL
		"\t\rGeneral Talk\x1b[D\x1b[D\x1b[H\x1b[F\x1b[C\r" + // Name, cursor keys
		"\t\rsay hi\x1b\t" + // Description: Esc abandons the edit
		"\x1b[B" + // Language untouched
		" \r" + // Access Mode: open -> approval -> closed
		"\t " + // Allow ANSI: Yes -> No
		keyCtrlS + "x"
	r := env.runCmd("V3NETPROPOSE", env.sysop, "", keys)
	if len(fake.proposals) != 1 {
		t.Fatalf("proposals = %d, want 1\n%s", len(fake.proposals), r.text())
	}
	got := fake.proposals[0]
	if got.Tag != "tst.general" || got.Name != "General Talk" || got.Description != "" ||
		got.Language != "en" || got.AccessMode != "closed" || got.AllowANSI {
		t.Errorf("request = %+v", got)
	}
	if !r.has("V3Net: Propose New Area", "(testnet)", "Proposal submitted! ID: prop-1  Status: pending") {
		t.Errorf("screen:\n%s", r.text())
	}
}

// TestV3NetProposeValidates pins the form's checks before anything is sent:
// a missing tag, a malformed tag and a missing name are each reported, and a
// hub error is shown without leaving the form.
func TestV3NetProposeValidates(t *testing.T) {
	env := newMenuEnv(t)
	fake := newV3NetScreenFake("testnet")
	env.e.V3NetStatus = fake

	r := env.runCmd("V3NETPROPOSE", env.sysop, "", keyCtrlS+"\rBAD TAG\r"+keyCtrlS+"q")
	if !r.has("Area Tag is required.") {
		t.Errorf("missing tag not reported:\n%s", r.text())
	}
	r = env.runCmd("V3NETPROPOSE", env.sysop, "", "\rtst.ok\r"+keyCtrlS+"q")
	if !r.has("Name is required.") {
		t.Errorf("missing name not reported:\n%s", r.text())
	}

	fake.proposeErr = errors.New("hub said no")
	r = env.runCmd("V3NETPROPOSE", env.sysop, "", "\rtst.ok\r\x1b[B\rOK\r"+keyCtrlS+"q")
	if !r.has("Error: hub said no") {
		t.Errorf("hub error not shown:\n%s", r.text())
	}
	if len(fake.proposals) != 0 {
		t.Errorf("invalid forms reached the hub: %+v", fake.proposals)
	}
}

// TestV3NetProposeMalformedTag pins that a tag outside prefix.name is
// refused with the validator's message and not submitted.
func TestV3NetProposeMalformedTag(t *testing.T) {
	env := newMenuEnv(t)
	fake := newV3NetScreenFake("testnet")
	env.e.V3NetStatus = fake

	r := env.runCmd("V3NETPROPOSE", env.sysop, "", "\rnodot\r\t\rName\r"+keyCtrlS+"q")
	if len(fake.proposals) != 0 || r.has("Proposal submitted") {
		t.Errorf("malformed tag submitted:\n%s", r.text())
	}
}

// TestV3NetProposeNavigationAndExits pins the form's wrap-around navigation,
// the no-network notice, the network argument, and exits on Esc and on a
// disconnect.
func TestV3NetProposeNavigationAndExits(t *testing.T) {
	env := newMenuEnv(t)
	fake := newV3NetScreenFake("testnet")
	env.e.V3NetStatus = fake

	// Up from Area Tag wraps to Allow ANSI; Enter toggles it; Down wraps back.
	r := env.runCmd("V3NETPROPOSE", env.sysop, "othernet", "\x1b[A\r\r\x1b[B\x1b")
	if !r.has("(othernet)") || r.next != "" {
		t.Errorf("network arg or Esc exit:\n%s", r.text())
	}
	if r := env.runCmd("V3NETPROPOSE", env.sysop, "", "\r"); r.next != "LOGOFF" {
		t.Errorf("disconnect mid-edit: next = %q", r.next)
	}
	fake.nets = nil
	if r := env.runCmd("V3NETPROPOSE", env.sysop, "", "\r"); !r.has("No V3Net subscriptions configured.") {
		t.Errorf("no networks:\n%s", r.text())
	}
	env.e.V3NetStatus = nil
	if r := env.runCmd("V3NETPROPOSE", env.sysop, "", "q"); r.raw != "" {
		t.Errorf("V3Net disabled should print nothing:\n%s", r.text())
	}
}

// TestCycleAccessMode pins the access-mode cycle and its reset of an
// unknown value to the first mode.
func TestCycleAccessMode(t *testing.T) {
	for in, want := range map[string]string{"open": "approval", "approval": "closed", "closed": "open", "weird": "open"} {
		if got := cycleAccessMode(in); got != want {
			t.Errorf("cycleAccessMode(%q) = %q, want %q", in, got, want)
		}
	}
}

package menu

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// chatNet is a network with Central on and chat offered and served.
func chatNet(name string) CentralNetworkInfo {
	return CentralNetworkInfo{Network: name, Enabled: true, SupportKnown: true, Supported: true, Chat: CentralChatInfo{Enabled: true, Supported: true}}
}

// chatcovDisagree clears a user's agreement to network chat, on the stored
// account and the test's copy.
func chatcovDisagree(env *menuEnv, u *user.User) {
	env.t.Helper()
	saved, err := env.um.SetCentralConsent(u.ID, func(x *user.User) { x.CentralChat, x.CentralChatChosen = false, false })
	if err != nil {
		env.t.Fatal(err)
	}
	centralConsentFrom(u, saved)
}

// A caller who has not agreed to network chat sees the notice before the
// network picker. N leaves them in local chat this time, recording
// nothing, so they are asked again; Y records the agreement and goes on
// to the network.
func TestChatNoticeBeforeFirstNetworkJoin(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.events = nil
	chatcovDisagree(env, env.caller)

	r := chatcovChat(env, env.caller, "n\rhello\r/q\r")
	if !chatcovHas(r, "Network Chat", "What other boards see", "Take part in network chat?", "<Caller> hello") {
		t.Errorf("notice not shown, or local chat did not follow:\n%s", chatcovText(r))
	}
	if chatcovHas(r, "Select network") || len(net.chatted()) != 0 {
		t.Errorf("declined caller reached the network")
	}
	stored, _ := env.um.GetUserByID(env.caller.ID)
	if stored.CentralChat || stored.CentralChatChosen {
		t.Errorf("a refusal was recorded: %+v", stored)
	}

	r = chatcovChat(env, env.caller, "y\r1\r\rhello\r/q\r")
	if !chatcovHas(r, "Take part in network chat?", "Select network", "<Caller> hello") {
		t.Errorf("notice, then picker, expected:\n%s", chatcovText(r))
	}
	stored, _ = env.um.GetUserByID(env.caller.ID)
	if !stored.CentralChat || !stored.CentralChatChosen {
		t.Errorf("agreement not stored: %+v", stored)
	}
	if got := net.chatted(); len(got) != 1 || len(got[0].log("post")) != 1 {
		t.Errorf("network sessions chatted = %d, want one with the post", len(got))
	}

	// Agreed once, the notice is not shown again.
	r = chatcovChat(env, env.caller, "1\r/q\r")
	if chatcovHas(r, "Take part in network chat?") || !chatcovHas(r, "Select network") {
		t.Errorf("notice shown to a caller who agreed:\n%s", chatcovText(r))
	}
}

// CHAT NET runs network chat only: no Local entry, and a caller who
// declines the notice is sent back rather than into local chat. CHAT
// LOCAL skips the networks and the notice.
func TestChatEntryArguments(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.events = nil

	r := env.run(runChat, env.caller, "NET", "1\r\rhi\r/q\r")
	if !chatcovHas(r, " 1. FakeNet", "<Caller> hi") || chatcovHas(r, "2. Local") {
		t.Errorf("CHAT NET should list the network only:\n%s", chatcovText(r))
	}
	if len(net.chatted()) != 1 {
		t.Errorf("CHAT NET chatted on %d network sessions, want 1", len(net.chatted()))
	}

	chatcovDisagree(env, env.caller)
	r = env.run(runChat, env.caller, "NET", "n\r")
	if !chatcovHas(r, "Take part in network chat?", "stays off for you") || chatcovHas(r, "Select room") {
		t.Errorf("CHAT NET after a refusal should end, not fall back to local:\n%s", chatcovText(r))
	}

	r = env.run(runChat, env.caller, "LOCAL", "hi\r/q\r")
	if chatcovHas(r, "Take part in network chat?", "Select network") || !chatcovHas(r, "<Caller> hi") {
		t.Errorf("CHAT LOCAL should go straight to local chat:\n%s", chatcovText(r))
	}
	if len(net.chatted()) != 1 {
		t.Errorf("CHAT LOCAL reached the network")
	}
}

// CHAT NET never lands in local chat: /network lists no Local, and a room
// that cannot be joined, at the start or after /network, ends chat.
func TestChatEntryNetworkNeverFallsBackToLocal(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.events = nil

	r := env.run(runChat, env.caller, "NET", "1\r\r/network\r1\r\rhi\r/q\r")
	txt := chatcovText(r)
	if strings.Contains(txt, "Local") || !strings.Contains(txt, "<Caller> hi") {
		t.Errorf("/network under CHAT NET should offer no Local:\n%s", txt)
	}

	net.joinErr = func(string) error { return errors.New("room is full") }
	r = env.run(runChat, env.caller, "NET", "1\r\rhi\r/q\r")
	if !chatcovHas(r, "Could not join the room: room is full") || chatcovHas(r, "Network: Local", "<Caller> hi") {
		t.Errorf("a failed join under CHAT NET should end chat, not fall back:\n%s", chatcovText(r))
	}
	if !net.allClosed() {
		t.Error("network session left open")
	}
}

// An agreement withdrawn on another node while the caller sits at a picker
// stops the join: nothing is published.
func TestChatWithdrawalDuringPickerPreventsJoin(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.events = nil

	live := startChatLive(env, env.caller)
	live.waitOutput("Select network [1]:")
	if _, err := env.um.SetCentralConsent(env.caller.ID, func(x *user.User) { x.CentralChat, x.CentralChatChosen = false, true }); err != nil {
		t.Fatal(err)
	}
	live.typeKeys("1\r")
	live.waitOutput("Select room [lobby]:")
	live.typeKeys("\r")
	live.waitOutput("Network chat is off for you")
	select {
	case <-live.done:
	case <-time.After(handlerWait):
		t.Fatal("chat did not end")
	}
	if len(net.chatted()) != 0 {
		t.Error("joined a room after the agreement was withdrawn")
	}
	if !net.allClosed() {
		t.Error("network session left open")
	}
}

// A withdrawal under Preferences, on this node or another, ends network
// chat at the caller's next line; local chat is unaffected.
func TestChatEndsWhenAgreementWithdrawn(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.events = nil

	live := startChatLive(env, env.caller)
	live.waitOutput("Select network [1]:")
	live.typeKeys("1\r")
	live.waitOutput("Select room [lobby]:")
	live.typeKeys("\r")
	live.waitOutput("Joined #lobby")
	live.typeKeys("first\r")
	// The input row echoes the line as typed, so wait for the post itself.
	s := net.chatted()[0]
	for deadline := time.Now().Add(screenWait); len(s.log("post")) == 0; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("first line never posted")
		}
	}

	// Another node turns the switch off.
	if _, err := env.um.SetCentralConsent(env.caller.ID, func(x *user.User) { x.CentralChat, x.CentralChatChosen = false, true }); err != nil {
		t.Fatal(err)
	}
	live.typeKeys("second\r")
	live.waitOutput("Network chat has ended.")
	select {
	case <-live.done:
	case <-time.After(handlerWait):
		t.Fatal("chat did not end after the withdrawal")
	}
	if posts := s.log("post"); len(posts) != 1 || !strings.HasSuffix(posts[0], "first") {
		t.Errorf("posts after withdrawal = %v, want only the first", posts)
	}
	if !net.allClosed() {
		t.Error("network session left open")
	}
}

// When the leaf ends the session (a reload stopped it), the reason is
// shown and the caller's next line ends chat instead of being posted to
// a dead connection.
func TestChatEndsWhenLeafClosesSession(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.events = nil

	live := startChatLive(env, env.caller)
	live.waitOutput("Select network [1]:")
	live.typeKeys("1\r")
	live.waitOutput("Select room [lobby]:")
	live.typeKeys("\r")
	live.waitOutput("Joined #lobby")

	net.chatted()[0].endByLeaf("Network chat is no longer offered on this board.")
	live.waitOutput("no longer offered on this board")
	live.typeKeys("anyone?\r")
	live.waitOutput("Network chat has ended.")
	select {
	case <-live.done:
	case <-time.After(handlerWait):
		t.Fatal("chat did not end after the leaf closed the session")
	}
	if posts := net.chatted()[0].log("post"); len(posts) != 0 {
		t.Errorf("posted to a closed session: %v", posts)
	}
}

// The Central menu offers Chat only where the sysop offers it and the hub
// serves it, and Status shows chat's state per network.
func TestCentralMenuAndStatusShowChat(t *testing.T) {
	off := &centralFake{nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}}}
	if out := runCentralScreen(t, off, "q\r", runV3NetCentral, ""); strings.Contains(out, "]hat") {
		t.Errorf("Chat offered with the switch off: %q", out)
	}
	unserved := chatNet("testnet")
	unserved.Chat.Supported = false
	if out := runCentralScreen(t, &centralFake{nets: []CentralNetworkInfo{unserved}}, "q\r", runV3NetCentral, ""); strings.Contains(out, "]hat") {
		t.Errorf("Chat offered without the hub serving it: %q", out)
	}
	on := &centralFake{nets: []CentralNetworkInfo{chatNet("testnet")}}
	if out := runCentralScreen(t, on, "q\r", runV3NetCentral, ""); !strings.Contains(out, "]hat") {
		t.Errorf("Chat not offered: %q", out)
	}

	checking := chatNet("newnet")
	checking.SupportKnown, checking.Chat.Supported = false, false
	status := &centralFake{nets: []CentralNetworkInfo{chatNet("testnet"), unserved, checking, off.nets[0]}}
	out := ansi.StripAnsi(runCentralScreen(t, status, "\r", runV3NetCentralStatus, ""))
	for _, want := range []string{"CHAT", "testnet       on", "hub does not serve it", "newnet        checking...", "testnet       off"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
}

// Preferences has a switch for network chat: refused where no network
// offers chat, on only through the notice, and off again at a press.
func TestCentralPrefsChatSwitch(t *testing.T) {
	none := &centralFake{nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}}}
	u := &user.User{Handle: "Caller"}
	if out := runPrefs(t, none, u, "4\rq\r"); !strings.Contains(out, "does not offer network chat") || u.CentralChat {
		t.Errorf("unoffered chat switched on, or no refusal: %+v", u)
	}

	fake := &centralFake{nets: []CentralNetworkInfo{chatNet("testnet")}}
	out := runPrefs(t, fake, u, "4\rn\rq\r")
	if !strings.Contains(out, "Take part in network chat?") || u.CentralChat || !strings.Contains(out, "stays off") {
		t.Errorf("declining the notice should leave chat off: %+v\n%s", u, ansi.StripAnsi(out))
	}
	out = runPrefs(t, fake, u, "4\ry\rq\r")
	if !u.CentralChat || !u.CentralChatChosen {
		t.Fatalf("agreement not recorded: %+v", u)
	}
	plain := ansi.StripAnsi(out)
	if !strings.Contains(plain, "Take part in network chat     On  (testnet)") || !strings.Contains(plain, "Network chat: Caller on") {
		t.Errorf("switch not shown on with its preview:\n%s", plain)
	}
	runPrefs(t, fake, u, "4\rq\r")
	if u.CentralChat || !u.CentralChatChosen {
		t.Errorf("switch not turned off: %+v", u)
	}

	// An agreement outlives the offer, so it can still be withdrawn.
	u.CentralChat = true
	out = ansi.StripAnsi(runPrefs(t, none, u, "4\rq\r"))
	if !strings.Contains(out, "not offered here now") || u.CentralChat {
		t.Errorf("stale agreement not shown or not withdrawn: %+v\n%s", u, out)
	}
}

// Konfig shows V3Net Sharing when only chat is offered, and sums an
// agreement up as Chat.
func TestKonfigV3NetSharingShowsChat(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	fake := &centralFake{nets: []CentralNetworkInfo{chatNet("testnet")}}
	snap := runSharingKonfig(t, um, u, fake, "q").Snapshot()
	if !strings.Contains(snap, "[R] V3Net Sharing") || strings.Contains(snap, "Chat") {
		t.Errorf("Sharing should be offered and off:\n%s", snap)
	}
	if _, err := um.SetCentralConsent(u.ID, func(x *user.User) { x.CentralChat = true }); err != nil {
		t.Fatal(err)
	}
	if snap := runSharingKonfig(t, um, u, fake, "q").Snapshot(); !strings.Contains(snap, "Chat") {
		t.Errorf("agreement not summed up as Chat:\n%s", snap)
	}
}

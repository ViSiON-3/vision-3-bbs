package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// chatcovNode registers a session for u on node in env's registry and returns
// it, so a test can page it and then read its queue. The harness always runs
// the handler under test as node 1.
func chatcovNode(env *menuEnv, node int, u *user.User) *session.BbsSession {
	sess := &session.BbsSession{NodeID: node, User: u}
	env.e.SessionRegistry.Register(sess)
	return sess
}

func TestChatcovPageDeliversToTargetNode(t *testing.T) {
	env := newMenuEnv(t)
	chatcovNode(env, 1, env.caller)
	target := chatcovNode(env, 2, env.sysop)
	chatcovNode(env, 3, nil) // connected but not logged in yet

	r := env.run(runPage, env.caller, "", "2\r  lunch?  \r")
	if r.err != nil || r.next != "" {
		t.Fatalf("runPage = (%q, %v), want no action and no error", r.next, r.err)
	}
	if !r.has("Online Nodes:", "Node 2: Sysop", "Node 3: Unknown", "Page sent to Node 2.") {
		t.Errorf("output missing node list or confirmation:\n%s", r.text())
	}
	// The caller's own node is left out of the list they pick from.
	if r.has("Node 1:") {
		t.Errorf("node list includes the caller's own node:\n%s", r.text())
	}

	// The page is queued on the target with the message trimmed, and nowhere else.
	pages := target.DrainPages()
	if len(pages) != 1 || !strings.Contains(pages[0], "Page from |15Caller|09: lunch?|07") {
		t.Errorf("target pages = %q, want one page from Caller reading \"lunch?\"", pages)
	}
	if got := env.e.SessionRegistry.Get(3).DrainPages(); len(got) != 0 {
		t.Errorf("node 3 received pages it was not sent: %q", got)
	}
}

func TestChatcovPageRejectsBadTargets(t *testing.T) {
	env := newMenuEnv(t)
	chatcovNode(env, 1, env.caller)
	target := chatcovNode(env, 2, env.sysop)

	for _, tc := range []struct {
		name, input, want string
	}{
		{"not a number", "two\r", "Invalid node number."},
		{"own node", "1\r", "You can't page yourself."},
		{"nobody on that node", "9\r", "That node is not online."},
		{"empty message", "2\r   \r", "Page cancelled."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := env.sub(t).run(runPage, env.caller, "", tc.input)
			if r.err != nil || r.next != "" {
				t.Fatalf("runPage = (%q, %v), want no action and no error", r.next, r.err)
			}
			if !r.has(tc.want) {
				t.Errorf("output missing %q:\n%s", tc.want, r.text())
			}
			if r.has("Page sent") {
				t.Errorf("page reported as sent:\n%s", r.text())
			}
		})
	}
	if pages := target.DrainPages(); len(pages) != 0 {
		t.Errorf("rejected pages were still queued: %q", pages)
	}
}

func TestChatcovPageCancelAtNodePrompt(t *testing.T) {
	env := newMenuEnv(t)
	target := chatcovNode(env, 2, env.sysop)

	for _, input := range []string{"q\r", "Q\r", "\r"} {
		r := env.run(runPage, env.caller, "", input)
		if r.err != nil || r.next != "" {
			t.Fatalf("input %q: runPage = (%q, %v), want no action and no error", input, r.next, r.err)
		}
		if !r.has("Page which node?") {
			t.Errorf("input %q: node prompt not shown:\n%s", input, r.text())
		}
		if r.has("Message:") || r.has("Invalid node") {
			t.Errorf("input %q: cancel went past the node prompt:\n%s", input, r.text())
		}
	}
	if pages := target.DrainPages(); len(pages) != 0 {
		t.Errorf("cancelled pages were queued: %q", pages)
	}
}

// An invisible session is hidden from ordinary callers both in the list and
// as a target: paging it reports the node as offline rather than confirming
// someone is there. CoSysOps and above see and reach it as usual.
func TestChatcovPageInvisibleNode(t *testing.T) {
	env := newMenuEnv(t)
	env.seedUsers(&user.User{ID: 3, Handle: "Ghost", AccessLevel: 255, Validated: true, TimeLimit: 60})
	ghostUser, _ := env.um.GetUserByID(3)
	ghost := chatcovNode(env, 2, ghostUser)
	ghost.Invisible = true

	r := env.run(runPage, env.caller, "", "2\rboo\r")
	if r.has("Ghost") || r.has("Message:") {
		t.Errorf("invisible node shown to an ordinary caller:\n%s", r.text())
	}
	if !r.has("That node is not online.") {
		t.Errorf("paging an invisible node should report it offline:\n%s", r.text())
	}
	if pages := ghost.DrainPages(); len(pages) != 0 {
		t.Errorf("ordinary caller paged an invisible node: %q", pages)
	}

	r = env.run(runPage, env.sysop, "", "2\rboo\r")
	if !r.has("Node 2: Ghost", "Page sent to Node 2.") {
		t.Errorf("sysop should see and reach the invisible node:\n%s", r.text())
	}
	if pages := ghost.DrainPages(); len(pages) != 1 || !strings.Contains(pages[0], "Sysop") {
		t.Errorf("ghost pages = %q, want one page from Sysop", pages)
	}
}

func TestChatcovPageDisconnects(t *testing.T) {
	env := newMenuEnv(t)
	target := chatcovNode(env, 2, env.sysop)

	// Not logged in: nothing to do, nothing shown.
	if r := env.run(runPage, nil, "", "2\rhi\r"); r.raw != "" || r.next != "" {
		t.Errorf("runPage with no user = (%q, output %q), want nothing", r.next, r.raw)
	}

	// Input ending at either prompt is a dropped connection.
	for _, input := range []string{"", "2\r", "2\rhalf a mess"} {
		r := env.run(runPage, env.caller, "", input)
		if r.next != "LOGOFF" {
			t.Errorf("input %q: next = %q, want LOGOFF", input, r.next)
		}
	}
	if pages := target.DrainPages(); len(pages) != 0 {
		t.Errorf("a dropped connection still queued pages: %q", pages)
	}
}

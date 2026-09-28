package menu

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// SCANNUV refuses when voting is off or the caller is below nuvUseLevel, and
// says so when the queue is empty; with no user it does nothing.
func TestSCANNUV_Gates(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("SCANNUV", nil, "", ""); r.user != nil || r.raw != "" {
		t.Errorf("no user: user=%v out=%q", r.user, r.raw)
	}
	if r := env.runCmd("SCANNUV", env.sysop, "", "\r"); !r.has("New User Voting is disabled.") {
		t.Errorf("disabled: %q", r.text())
	}
	enableNUV(env, 2, 5, true, false)
	if r := env.runCmd("SCANNUV", env.caller, "", "\r"); !r.has("do not have access") {
		t.Errorf("below level: %q", r.text())
	}
	if r := env.runCmd("SCANNUV", env.sysop, "", "\r"); !r.has("No NEW users right now!") {
		t.Errorf("empty queue: %q", r.text())
	}
	// A corrupt queue is logged and skipped rather than shown as empty.
	if err := os.WriteFile(nuvFilePath(env.cfgDir()), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := env.runCmd("SCANNUV", env.sysop, "", ""); r.has("No NEW users right now!") || r.err != nil {
		t.Errorf("corrupt queue: err=%v out=%q", r.err, r.text())
	}
}

// SCANNUV only visits candidates the caller has not voted on, and says so
// when there are none left.
func TestSCANNUV_SkipsAlreadyVoted(t *testing.T) {
	env := newMenuEnv(t)
	enableNUV(env, 5, 5, true, false)
	seedNUV(t, env, NUVCandidate{Handle: "Voted", When: time.Now(), Votes: []NUVVote{{Voter: "sysop", Yes: true}}})

	r := env.runCmd("SCANNUV", env.sysop, "", "")
	if !r.has("No New Users Found!") || r.has("Candidate #") {
		t.Errorf("scan with nothing unvoted: %q", r.text())
	}
}

// CHECKNUV at login offers the scan only to a qualified voter with unvoted
// candidates, counting them; answering Y runs the scan.
func TestCHECKNUV_OffersScan(t *testing.T) {
	env := newMenuEnv(t)
	enableNUV(env, 5, 5, true, false)
	seedNUV(t, env,
		NUVCandidate{Handle: "One", When: time.Now()},
		NUVCandidate{Handle: "Two", When: time.Now(), Votes: []NUVVote{{Voter: "Sysop", Yes: true}}})

	if r := env.run(runCheckNUV, env.caller, "", "Y"); r.has("voted on") {
		t.Errorf("below-level caller offered NUV: %q", r.text())
	}
	r := env.run(runCheckNUV, env.sysop, "", "N")
	if !r.has("You have NOT voted on 1 New Users.") || r.has("Candidate #") {
		t.Errorf("declined offer: %q", r.text())
	}
	// The shipped prompt's trailing " @" is the Yes/No lightbar marker: the
	// caller gets the lightbar, never a literal "@".
	if r.has(" @") || !r.has("Vote Now?", "No") {
		t.Errorf("vote prompt not shown as a Yes/No lightbar: %q", r.text())
	}
	r = env.run(runCheckNUV, env.sysop, "", "YY\rQ")
	if !r.has("New User Voting - Candidate #1", "One") {
		t.Errorf("accepted offer did not scan: %q", r.text())
	}
	if v := readNUV(t, env).Candidates[0].Votes; len(v) != 1 || !v[0].Yes {
		t.Errorf("vote from CHECKNUV scan = %+v", v)
	}
	if r.user != env.sysop {
		t.Error("CHECKNUV did not keep the session user")
	}

	// Enter takes the lightbar's default, No.
	if r := env.run(runCheckNUV, env.sysop, "", "\rQ"); r.has("Candidate #") {
		t.Errorf("Enter at the vote prompt ran the scan: %q", r.text())
	}

	setServerField(env.e, func(c *config.ServerConfig) { c.UseNUV = false })
	if r := env.run(runCheckNUV, env.sysop, "", "Y"); r.raw != "" {
		t.Errorf("NUV off still offered: %q", r.text())
	}
}

// LISTNUV shows every candidate with tallies and whether the viewer voted; a
// non-sysop only views it.
func TestLISTNUV_ListsTallies(t *testing.T) {
	env := newMenuEnv(t)
	seedNUV(t, env, NUVCandidate{Handle: "Newbie", When: time.Now(), Votes: []NUVVote{
		{Voter: "Caller", Yes: true}, {Voter: "X", Yes: false}, {Voter: "Y", Yes: true},
	}})
	r := env.runCmd("LISTNUV", env.caller, "", "\r")
	if !r.has("New User Voting Queue - 1 Candidate(s)", "Newbie") {
		t.Fatalf("list: %q", r.text())
	}
	if !lineHasInOrder(r.text(), "Newbie", "2", "1", "Yes") {
		t.Errorf("tally row wrong: %q", r.text())
	}
	if r.has("[A]dd") {
		t.Error("non-sysop offered queue management")
	}
	if r := env.runCmd("LISTNUV", nil, "", ""); r.raw != "" {
		t.Errorf("no user: %q", r.raw)
	}
}

// A sysop can add an existing account to the queue (unknown handles are
// refused), remove a candidate by number (bad numbers are refused), and quit.
func TestLISTNUV_SysopManagesQueue(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("LISTNUV", env.sysop, "", "ANobody\rACaller\rA\rR9\rRx\rV0\rV5\rV\rZQ")
	if !r.has("No candidates pending.", "User 'Nobody' not found.", "Added 'Caller' to NUV queue.",
		"Invalid candidate number.", "Invalid number.") {
		t.Errorf("manage: %q", r.text())
	}
	nd := readNUV(t, env)
	if len(nd.Candidates) != 1 || nd.Candidates[0].Handle != "Caller" {
		t.Fatalf("queue = %+v, want [Caller]", nd.Candidates)
	}

	r = env.runCmd("LISTNUV", env.sysop, "", "R1\rQ")
	if !r.has("Removed 'Caller' from queue.") {
		t.Errorf("remove: %q", r.text())
	}
	if n := len(readNUV(t, env).Candidates); n != 0 {
		t.Errorf("queue has %d after removal", n)
	}
}

// lineHasInOrder reports whether some line of txt contains every want, in
// order.
func lineHasInOrder(txt string, want ...string) bool {
	for _, line := range strings.Split(txt, "\n") {
		rest, ok := line, true
		for _, w := range want {
			j := strings.Index(rest, w)
			if j < 0 {
				ok = false
				break
			}
			rest = rest[j+len(w):]
		}
		if ok {
			return true
		}
	}
	return false
}

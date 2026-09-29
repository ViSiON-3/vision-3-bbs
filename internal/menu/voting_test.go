package menu

import (
	"errors"
	"slices"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// The voting booth prompt used to run past 80 columns for a sysop and wrap
// onto a second line. With every option shown and a three-digit topic count
// it must still leave room on the line for the reply.
func TestVoteBoothPromptFitsOneLine(t *testing.T) {
	const maxWidth = 70 // 80 columns less room to type
	for _, tc := range []struct {
		voted, sysop bool
	}{
		{false, false}, {true, false}, {false, true}, {true, true},
	} {
		p := string(ansi.ReplacePipeCodes([]byte(voteBoothPrompt(tc.voted, tc.sysop, 999))))
		if w := ansi.VisibleLength(p); w > maxWidth {
			t.Errorf("voted=%v sysop=%v: prompt is %d columns, want <= %d: %q", tc.voted, tc.sysop, w, maxWidth, p)
		}
	}
}

// seedVoting writes voting.json directly.
func seedVoting(t *testing.T, env *menuEnv, topics ...VoteTopic) {
	t.Helper()
	for i := range topics {
		if topics[i].Votes == nil {
			topics[i].Votes = map[string][]string{}
		}
	}
	if err := saveVotingData(env.dataDir(), &VotingData{Topics: topics}); err != nil {
		t.Fatal(err)
	}
}

// readVoting loads voting.json.
func readVoting(t *testing.T, env *menuEnv) *VotingData {
	t.Helper()
	vd, err := loadVotingData(env.dataDir())
	if err != nil {
		t.Fatal(err)
	}
	return vd
}

// VOTE with no topics tells a regular caller so; a sysop may create the first
// topic, which is saved with its options, mandatory flag and add level.
func TestVote_NoTopicsAndSysopCreatesFirst(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("VOTE", nil, "", ""); r.raw != "" || r.user != nil {
		t.Errorf("no user: %q", r.raw)
	}
	r := env.runCmd("VOTE", env.caller, "", "\r")
	if !r.has("No voting topics right now.") || r.has("Create first topic") {
		t.Errorf("caller, no topics: %q", r.text())
	}

	// Declining, or entering no choices, creates nothing.
	env.runCmd("VOTE", env.sysop, "", "N\r")
	r = env.runCmd("VOTE", env.sysop, "", "Y\rBest color?\rN\rN\r\r\r")
	if !r.has("No choices entered, topic not created.") {
		t.Errorf("no choices: %q", r.text())
	}
	if n := len(readVoting(t, env).Topics); n != 0 {
		t.Fatalf("topics = %d after aborted creates", n)
	}

	r = env.runCmd("VOTE", env.sysop, "", "Y\rBest color?\rY\rY\r10\rRed\rBlue\r\rQ\r")
	if !r.has("Topic created!", "Best color?") {
		t.Errorf("create: %q", r.text())
	}
	vd := readVoting(t, env)
	if len(vd.Topics) != 1 {
		t.Fatalf("topics = %+v", vd.Topics)
	}
	tp := vd.Topics[0]
	if tp.ID != 1 || tp.Question != "Best color?" || !tp.Mandatory || tp.AddLevel != 10 ||
		!slices.Equal(tp.Options, []string{"Red", "Blue"}) {
		t.Errorf("topic = %+v", tp)
	}
}

// A caller votes once: the vote is saved under their handle and results are
// shown; a second vote is refused, results are gated on having voted, and an
// out-of-range choice is not recorded.
func TestVote_CastOnceAndResults(t *testing.T) {
	env := newMenuEnv(t)
	seedVoting(t, env, VoteTopic{ID: 1, Question: "Best color?", Options: []string{"Red", "Blue"}})

	r := env.runCmd("VOTE", env.caller, "", "R\rV\r9\rV\r2\r\rV\rR\r\rQ\r")
	if !r.has("Sorry, you must vote first!", "Invalid selection. Vote not recorded.",
		"Thanks for voting!", "Sorry, can't vote twice!!", "[Voted]", "(1 vote)", "100.0%") {
		t.Errorf("vote flow: %q", r.text())
	}
	votes := readVoting(t, env).Topics[0].Votes
	if !slices.Equal(votes["1"], []string{"Caller"}) || len(votes["0"]) != 0 {
		t.Errorf("votes = %v, want Caller on option 2", votes)
	}
}

// The booth navigates topics with N (wrapping) and by number, and L lists the
// current topic's choices.
func TestVote_NavigateAndList(t *testing.T) {
	env := newMenuEnv(t)
	seedVoting(t, env,
		VoteTopic{ID: 1, Question: "First?", Options: []string{"A1"}},
		VoteTopic{ID: 2, Question: "Second?", Options: []string{"B1", "B2"}})

	r := env.runCmd("VOTE", env.caller, "", "N\rL\r\rN\r2\r99\rQ\r")
	txt := r.text()
	if !r.has("Current topic [2]: Second?", "Current topic [1]: First?", "B2") {
		t.Errorf("navigation: %q", txt)
	}
	if r.has("[A]dd") {
		t.Error("non-sysop offered topic management")
	}
}

// A caller at or above a topic's add level can add a choice, which is saved
// but does not count as a vote.
func TestVote_UserAddsChoice(t *testing.T) {
	env := newMenuEnv(t)
	seedVoting(t, env,
		VoteTopic{ID: 1, Question: "Open?", Options: []string{"One"}, AddLevel: 10},
		VoteTopic{ID: 2, Question: "Closed?", Options: []string{"One"}, AddLevel: 50})

	r := env.runCmd("VOTE", env.caller, "", "V\rA\rTwo\rQ\r")
	if !r.has("Choice added!") {
		t.Errorf("add choice: %q", r.text())
	}
	// On the topic above the caller's level, A is just an invalid selection.
	r = env.runCmd("VOTE", env.caller, "", "2\rV\rA\rQ\r")
	if !r.has("Invalid selection.") {
		t.Errorf("add above level: %q", r.text())
	}
	vd := readVoting(t, env)
	if !slices.Equal(vd.Topics[0].Options, []string{"One", "Two"}) || !slices.Equal(vd.Topics[1].Options, []string{"One"}) {
		t.Errorf("options = %v / %v", vd.Topics[0].Options, vd.Topics[1].Options)
	}
	if hasVoted(&vd.Topics[0], "Caller") {
		t.Error("adding a choice counted as a vote")
	}
}

// A sysop can delete the current topic after confirming; deleting the last
// one leaves the booth.
func TestVote_SysopDeletesTopics(t *testing.T) {
	env := newMenuEnv(t)
	seedVoting(t, env,
		VoteTopic{ID: 1, Question: "Keep?", Options: []string{"x"}},
		VoteTopic{ID: 2, Question: "Drop?", Options: []string{"y"}})

	env.runCmd("VOTE", env.sysop, "", "2\rD\rN\rD\rY\rQ\r")
	vd := readVoting(t, env)
	if len(vd.Topics) != 1 || vd.Topics[0].Question != "Keep?" {
		t.Fatalf("topics = %+v, want only Keep?", vd.Topics)
	}
	r := env.runCmd("VOTE", env.sysop, "", "D\rY\r\r")
	if !r.has("No voting topics right now.") {
		t.Errorf("last delete: %q", r.text())
	}
	if n := len(readVoting(t, env).Topics); n != 0 {
		t.Errorf("topics = %d", n)
	}
}

// VOTEMANDATORY walks only the mandatory topics the caller has not voted on
// and records their votes; an invalid pick is reported and not recorded.
func TestVoteMandatory(t *testing.T) {
	env := newMenuEnv(t)
	seedVoting(t, env,
		VoteTopic{ID: 1, Question: "Optional?", Options: []string{"a"}},
		VoteTopic{ID: 2, Question: "Must A?", Options: []string{"yes", "no"}, Mandatory: true},
		VoteTopic{ID: 3, Question: "Must B?", Options: []string{"yes"}, Mandatory: true,
			Votes: map[string][]string{"0": {"caller"}}},
		VoteTopic{ID: 4, Question: "Must C?", Options: []string{"yes"}, Mandatory: true})

	if r := env.runCmd("VOTEMANDATORY", nil, "", ""); r.raw != "" {
		t.Errorf("no user: %q", r.raw)
	}
	r := env.runCmd("VOTEMANDATORY", env.caller, "", "1\r\r7\r")
	if r.user != env.caller {
		t.Error("VOTEMANDATORY did not keep the session user")
	}
	if r.has("Optional?", "Must B?") {
		t.Errorf("showed a non-mandatory or already-voted topic: %q", r.text())
	}
	if !r.has("Mandatory Voting!", "Must A?", "Must C?", "Invalid selection. Vote not recorded.") {
		t.Errorf("mandatory flow: %q", r.text())
	}
	vd := readVoting(t, env)
	if !hasVoted(&vd.Topics[1], "Caller") || hasVoted(&vd.Topics[3], "Caller") || hasVoted(&vd.Topics[0], "Caller") {
		t.Errorf("votes = %+v", vd.Topics)
	}
}

// voteRecordVote refuses unknown topic IDs and out-of-range options and never
// records a second vote from the same handle, whatever its case.
func TestVoteRecordVoteBounds(t *testing.T) {
	env := newMenuEnv(t)
	seedVoting(t, env, VoteTopic{ID: 1, Question: "Q?", Options: []string{"a", "b"}})
	if _, err := voteRecordVote(env.dataDir(), 5, 0, "Caller"); !errors.Is(err, errVoteTopicGone) {
		t.Errorf("unknown topic ID: err = %v, want errVoteTopicGone", err)
	}
	if _, err := voteRecordVote(env.dataDir(), 1, 2, "Caller"); err == nil {
		t.Error("out-of-range option accepted")
	}
	if _, err := voteRecordVote(env.dataDir(), 1, 0, "Caller"); err != nil {
		t.Fatal(err)
	}
	if _, err := voteRecordVote(env.dataDir(), 1, 1, "CALLER"); err != nil {
		t.Fatal(err)
	}
	tp := readVoting(t, env).Topics[0]
	if totalVotes(&tp) != 1 || !slices.Equal(tp.Votes["0"], []string{"Caller"}) {
		t.Errorf("votes = %v, want one vote on a", tp.Votes)
	}
}

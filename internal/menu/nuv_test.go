package menu

import (
	"os"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// enableNUV turns on new-user voting with the given yes/no thresholds.
func enableNUV(env *menuEnv, yes, no int, validate, kill bool) {
	setServerField(env.e, func(c *config.ServerConfig) {
		c.UseNUV = true
		c.NUVUseLevel = 25
		c.NUVYesVotes = yes
		c.NUVNoVotes = no
		c.NUVValidate = validate
		c.NUVKill = kill
		c.NUVLevel = 30
		c.NUVForm = 0
	})
}

// seedNUV writes the NUV queue directly.
func seedNUV(t *testing.T, env *menuEnv, cands ...NUVCandidate) {
	t.Helper()
	if err := saveNUVData(env.dataDir(), &NUVData{Candidates: cands}); err != nil {
		t.Fatal(err)
	}
}

// readNUV loads the NUV queue from disk.
func readNUV(t *testing.T, env *menuEnv) *NUVData {
	t.Helper()
	nd, err := loadNUVData(env.dataDir())
	if err != nil {
		t.Fatal(err)
	}
	return nd
}

// addCandidateUser creates a level-10 unvalidated account and queues it.
func addCandidateUser(t *testing.T, env *menuEnv, handle string) {
	t.Helper()
	env.um.SetNewUserLevel(10)
	if _, err := env.um.AddUser("pw123", handle, handle+" Person", "Here"); err != nil {
		t.Fatal(err)
	}
	if err := nuvAddCandidate(env.dataDir(), handle); err != nil {
		t.Fatal(err)
	}
}

// The NUV queue lives in data/nuv.json next to the configs directory; a
// missing file is an empty queue, a candidate is queued once however its
// handle is cased, and a corrupt file is an error rather than an empty queue.
func TestNUVQueueStorage(t *testing.T) {
	env := newMenuEnv(t)
	if nd := readNUV(t, env); len(nd.Candidates) != 0 {
		t.Fatalf("fresh queue = %+v", nd.Candidates)
	}
	for _, h := range []string{"Newbie", "NEWBIE", "newbie"} {
		if err := nuvAddCandidate(env.dataDir(), h); err != nil {
			t.Fatal(err)
		}
	}
	nd := readNUV(t, env)
	if len(nd.Candidates) != 1 || nd.Candidates[0].Handle != "Newbie" || nd.Candidates[0].When.IsZero() {
		t.Fatalf("queue = %+v, want one Newbie with a timestamp", nd.Candidates)
	}
	if _, err := os.Stat(env.dataDir() + "/nuv.json"); err != nil {
		t.Errorf("nuv.json not in the data dir: %v", err)
	}

	if err := os.WriteFile(nuvFilePath(env.dataDir()), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadNUVData(env.dataDir()); err == nil {
		t.Error("corrupt nuv.json loaded without error")
	}
	if err := nuvAddCandidate(env.dataDir(), "Other"); err == nil {
		t.Error("adding to a corrupt queue did not fail")
	}
}

// Voting Yes on a candidate records the vote under the voter's handle, then
// offers a comment which is saved with it. Below the threshold the candidate
// stays queued and the account is untouched.
func TestNUVVote_YesWithCommentPersisted(t *testing.T) {
	env := newMenuEnv(t)
	enableNUV(env, 2, 5, true, false)
	addCandidateUser(t, env, "Newbie")

	r := env.runCmd("SCANNUV", env.sysop, "", "YLooks good\rQ")
	if !r.has("Newbie", "Yes Vote Cast!") {
		t.Errorf("missing vote screen: %q", r.text())
	}
	nd := readNUV(t, env)
	if len(nd.Candidates) != 1 {
		t.Fatalf("candidate removed below threshold: %+v", nd.Candidates)
	}
	v := nd.Candidates[0].Votes
	if len(v) != 1 || v[0].Voter != "Sysop" || !v[0].Yes || v[0].Comment != "Looks good" || v[0].VotedAt.IsZero() {
		t.Fatalf("votes = %+v", v)
	}
	if u := mustGetUser(t, env, "Newbie"); u.AccessLevel != 10 || u.Validated {
		t.Errorf("account changed below threshold: level=%d validated=%v", u.AccessLevel, u.Validated)
	}
}

// Reaching the Yes threshold with nuvValidate raises the account to nuvLevel,
// marks it validated and removes it from the queue.
func TestNUVVote_YesThresholdValidates(t *testing.T) {
	env := newMenuEnv(t)
	enableNUV(env, 1, 5, true, false)
	addCandidateUser(t, env, "Newbie")

	r := env.runCmd("SCANNUV", env.sysop, "", "Y")
	if !r.has("Threshold reached") {
		t.Errorf("no threshold message: %q", r.text())
	}
	if n := len(readNUV(t, env).Candidates); n != 0 {
		t.Errorf("queue has %d candidates, want 0", n)
	}
	if u := mustGetUser(t, env, "Newbie"); u.AccessLevel != 30 || !u.Validated {
		t.Errorf("level=%d validated=%v, want 30 / true", u.AccessLevel, u.Validated)
	}
}

// The other threshold outcomes: Yes without nuvValidate removes the candidate
// but leaves the account for the SysOp; No with nuvKill deletes the account;
// No without nuvKill only removes it from the queue.
func TestNUVVote_ThresholdOutcomes(t *testing.T) {
	cases := []struct {
		name           string
		key            string
		validate, kill bool
		wantDeleted    bool
	}{
		{"yes no-validate", "Y", false, false, false},
		{"no kill", "N", true, true, true},
		{"no keep", "N", true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newMenuEnv(t)
			enableNUV(env, 1, 1, tc.validate, tc.kill)
			addCandidateUser(t, env, "Newbie")

			env.runCmd("SCANNUV", env.sysop, "", tc.key)
			if n := len(readNUV(t, env).Candidates); n != 0 {
				t.Errorf("queue has %d candidates, want 0", n)
			}
			u := mustGetUser(t, env, "Newbie")
			if u.DeletedUser != tc.wantDeleted {
				t.Errorf("DeletedUser = %v, want %v", u.DeletedUser, tc.wantDeleted)
			}
			if u.AccessLevel != 10 || u.Validated {
				t.Errorf("account level/validation changed: %d / %v", u.AccessLevel, u.Validated)
			}
		})
	}
}

// Reaching the Yes threshold with nuvValidate for a handle that has no
// account keeps the candidate queued rather than silently dropping it.
func TestNUVVote_ValidateMissingAccountKeepsCandidate(t *testing.T) {
	env := newMenuEnv(t)
	enableNUV(env, 1, 5, true, false)
	seedNUV(t, env, NUVCandidate{Handle: "Ghost", When: time.Now()})

	env.runCmd("SCANNUV", env.sysop, "", "Y\r")
	nd := readNUV(t, env)
	if len(nd.Candidates) != 1 || len(nd.Candidates[0].Votes) != 1 {
		t.Errorf("queue = %+v, want Ghost kept with the vote", nd.Candidates)
	}
}

// The vote screen's other keys: comment before voting is refused, ? shows
// help, R reshows the stats with the voter's current vote, I reports when
// infoforms are not configured, and a changed vote replaces the old one
// without asking for another comment.
func TestNUVVote_ScreenKeysAndChangingVote(t *testing.T) {
	env := newMenuEnv(t)
	enableNUV(env, 5, 5, true, false)
	addCandidateUser(t, env, "Newbie")
	seedNUV(t, env, NUVCandidate{Handle: "Newbie", When: time.Now(), Votes: []NUVVote{
		{Voter: "Caller", Yes: false, Comment: "who?", VotedAt: time.Now()},
	}})

	// Sysop has not voted: C is refused.
	r := env.runCmd("LISTNUV", env.sysop, "", "V1\rC?IQQ")
	if !r.has("You have to Vote First!", "New User Voting Help", "Infoform viewing is not configured.", "who?") {
		t.Errorf("vote screen keys: %q", r.text())
	}

	// Sysop votes Yes (no comment), then changes to No and adds a comment.
	env.runCmd("LISTNUV", env.sysop, "", "V1\rY\rQQ")
	r = env.runCmd("LISTNUV", env.sysop, "", "V1\rRNCchanged mind\rQQ")
	if !r.has("Your Vote: Yes", "Vote changed to NO") {
		t.Errorf("change vote: %q", r.text())
	}
	nd := readNUV(t, env)
	votes := nd.Candidates[0].Votes
	if len(votes) != 2 {
		t.Fatalf("votes = %+v, want Caller and Sysop", votes)
	}
	if v := votes[nuvVoteIndex(&nd.Candidates[0], "sysop")]; v.Yes || v.Comment != "changed mind" {
		t.Errorf("sysop vote = %+v, want No with comment", v)
	}
	if yes := nuvYesCount(&nd.Candidates[0]); yes != 0 {
		t.Errorf("yes count = %d, want 0", yes)
	}
}

// I on the vote screen with an infoform configured shows that form for the
// candidate's account, or says the account is not in the user base.
func TestNUVVote_InfoformKey(t *testing.T) {
	env := newMenuEnv(t)
	enableNUV(env, 5, 5, true, false)
	setServerField(env.e, func(c *config.ServerConfig) { c.NUVForm = 1 })
	addCandidateUser(t, env, "Newbie")
	if err := nuvAddCandidate(env.dataDir(), "Ghost"); err != nil {
		t.Fatal(err)
	}

	r := env.runCmd("LISTNUV", env.sysop, "", "V1\rI\rQV2\rIQQ")
	if !r.has("That user has no information form.", "User not found in database.") {
		t.Errorf("infoform key: %q", r.text())
	}
}

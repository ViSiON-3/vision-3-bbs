package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// These tests cover file-backed features that let a sysop pick a record from a
// listing and then act on it after one or more prompts. Another node can change
// the same JSON file in between; the action must land on the record the sysop
// picked, keep the other node's change, and say so when the record is gone.
// "Another session" is simulated with testSession.whenOutput, which writes the
// file after a prompt is shown and before its reply is acted on.

// staleTestConfig returns a RootConfigPath whose sibling data directory exists
// and lives inside the test's temp dir.
func staleTestConfig(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "configs")
}

// runStaleScreen drives fn as a sysop with the scripted input. setup may
// register output hooks on the session before fn runs.
func runStaleScreen(t *testing.T, cfg, input string, setup func(ts *testSession), fn RunnableFunc) *testSession {
	t.Helper()
	e := &MenuExecutor{RootConfigPath: cfg}
	ts := newTestSession(input)
	if setup != nil {
		setup(ts)
	}
	t.Cleanup(func() { resetSessionIH(ts) })
	c := &cmdCtx{
		e: e, s: ts, terminal: newTestTerminal(ts),
		currentUser: &user.User{ID: 1, Handle: "Sysop", AccessLevel: 255, Validated: true},
		nodeNumber:  1,
		outputMode:  ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}
	_, _, _ = fn(c, "")
	return ts
}

func mustHookFire(t *testing.T, ts *testSession, marker string) {
	t.Helper()
	if !ts.hookFired(marker) {
		t.Fatalf("simulated other session never ran: marker %q not seen in output %q", marker, ts.output())
	}
}

// --- voting (#451) ---------------------------------------------------------

func voteTopicQuestions(t *testing.T, cfg string) map[string]int {
	t.Helper()
	vd, err := loadVotingData(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, tp := range vd.Topics {
		got[tp.Question] = tp.ID
	}
	return got
}

// Reproduces #451: after a delete, a new topic used to get ID len+1, which
// could be the ID of a live topic; deleting the new topic then removed the
// other one.
func TestVoteNewTopicAfterDeleteGetsUniqueID(t *testing.T) {
	cfg := staleTestConfig(t)
	if err := saveVotingData(cfg, &VotingData{Topics: []VoteTopic{
		{ID: 1, Question: "A?", Options: []string{"x"}, Votes: map[string][]string{}},
		{ID: 2, Question: "B?", Options: []string{"y"}, Votes: map[string][]string{}},
	}}); err != nil {
		t.Fatal(err)
	}

	// Delete A, add C, select topic 2 (C), delete it, quit.
	runStaleScreen(t, cfg, "D\rY\rA\rC?\rN\rN\rz\r\r2\rD\rY\rQ\r", nil, runVote)

	got := voteTopicQuestions(t, cfg)
	if _, ok := got["B?"]; !ok || len(got) != 1 {
		t.Fatalf("topics after deleting C = %v, want only B?", got)
	}
}

func TestVoteTopicIDsAreMaxPlusOne(t *testing.T) {
	vd := &VotingData{Topics: []VoteTopic{{ID: 2}, {ID: 7}, {ID: 3}}}
	if got := nextVoteTopicID(vd); got != 8 {
		t.Errorf("nextVoteTopicID = %d, want 8", got)
	}
	if got := nextVoteTopicID(&VotingData{}); got != 1 {
		t.Errorf("nextVoteTopicID(empty) = %d, want 1", got)
	}
}

// voting.json files written by the old allocator may already hold duplicate
// IDs. Loading must give each topic its own ID without moving any votes.
func TestLoadVotingDataRepairsDuplicateIDs(t *testing.T) {
	cfg := staleTestConfig(t)
	raw := `{"topics":[
		{"id":2,"question":"B?","options":["y","n"],"votes":{"0":["alice"]}},
		{"id":2,"question":"C?","options":["y","n"],"votes":{"1":["bob"]}},
		{"id":0,"question":"D?","options":["y"],"votes":null},
		{"id":1,"question":"E?","options":["y"],"votes":{}}
	]}`
	if err := os.WriteFile(votingFilePath(cfg), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	vd, err := loadVotingData(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]string{}
	for _, tp := range vd.Topics {
		if tp.ID <= 0 {
			t.Errorf("topic %q has ID %d", tp.Question, tp.ID)
		}
		if prev, dup := seen[tp.ID]; dup {
			t.Errorf("topics %q and %q share ID %d", prev, tp.Question, tp.ID)
		}
		seen[tp.ID] = tp.Question
	}
	if vd.Topics[0].ID != 2 || vd.Topics[3].ID != 1 {
		t.Errorf("first holders of an ID must keep it: got B?=%d E?=%d", vd.Topics[0].ID, vd.Topics[3].ID)
	}
	if v := vd.Topics[0].Votes["0"]; len(v) != 1 || v[0] != "alice" {
		t.Errorf("B? votes = %v, want alice on option 0", vd.Topics[0].Votes)
	}
	if v := vd.Topics[1].Votes["1"]; len(v) != 1 || v[0] != "bob" {
		t.Errorf("C? votes = %v, want bob on option 1", vd.Topics[1].Votes)
	}

	// The repair is deterministic, so a second load agrees with the first.
	again, err := loadVotingData(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := range vd.Topics {
		if vd.Topics[i].ID != again.Topics[i].ID {
			t.Errorf("topic %d: ID %d on first load, %d on second", i, vd.Topics[i].ID, again.Topics[i].ID)
		}
	}
}

// A vote used to be recorded by list position against freshly loaded data, so
// a topic deleted by another session shifted it onto the next topic.
func TestVoteLandsOnChosenTopicAfterConcurrentDelete(t *testing.T) {
	cfg := staleTestConfig(t)
	topics := []VoteTopic{
		{ID: 1, Question: "A?", Options: []string{"x"}, Votes: map[string][]string{}},
		{ID: 2, Question: "B?", Options: []string{"y"}, Votes: map[string][]string{}},
		{ID: 3, Question: "C?", Options: []string{"z"}, Votes: map[string][]string{}},
	}
	if err := saveVotingData(cfg, &VotingData{Topics: topics}); err != nil {
		t.Fatal(err)
	}
	const marker = "Your selection"
	ts := runStaleScreen(t, cfg, "2\rV\r1\r\rQ\r", func(ts *testSession) {
		ts.whenOutput(marker, func() {
			// Another sysop deletes A while this caller is choosing.
			if err := saveVotingData(cfg, &VotingData{Topics: topics[1:]}); err != nil {
				t.Error(err)
			}
		})
	}, runVote)
	mustHookFire(t, ts, marker)

	vd, err := loadVotingData(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, tp := range vd.Topics {
		switch tp.Question {
		case "B?":
			if !hasVoted(&tp, "Sysop") {
				t.Errorf("vote not recorded on B?, the chosen topic")
			}
		case "C?":
			if totalVotes(&tp) != 0 {
				t.Errorf("vote landed on C?, which was not chosen: %v", tp.Votes)
			}
		}
	}
}

// --- news (#452) -----------------------------------------------------------

func seedNewsItems(t *testing.T, cfg string, items ...NewsItem) {
	t.Helper()
	nd := &NewsData{Items: items}
	for _, it := range items {
		if it.ID >= nd.NextID {
			nd.NextID = it.ID + 1
		}
	}
	if err := saveNewsData(cfg, nd); err != nil {
		t.Fatal(err)
	}
}

func newsTitles(t *testing.T, cfg string) []string {
	t.Helper()
	nd, err := loadNewsData(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range nd.Items {
		out = append(out, it.Title)
	}
	return out
}

// prependNews plays another session adding a news item, which goes first.
func prependNews(t *testing.T, cfg, title string) {
	newsMu.Lock()
	defer newsMu.Unlock()
	nd, err := loadNewsData(cfg)
	if err != nil {
		t.Error(err)
		return
	}
	nd.Items = append([]NewsItem{{ID: allocNewsID(nd), Title: title, Body: "b"}}, nd.Items...)
	if err := saveNewsData(cfg, nd); err != nil {
		t.Error(err)
	}
}

func TestNewsDeleteHitsChosenItemAfterConcurrentAdd(t *testing.T) {
	cfg := staleTestConfig(t)
	seedNewsItems(t, cfg, NewsItem{ID: 1, Title: "Old", Body: "b"}, NewsItem{ID: 2, Title: "Older", Body: "b"})

	const marker = "Delete #1 ("
	ts := runStaleScreen(t, cfg, "D\r1\rY\rQ\r", func(ts *testSession) {
		ts.whenOutput(marker, func() { prependNews(t, cfg, "Fresh") })
	}, runEditNews)
	mustHookFire(t, ts, marker)

	if got := strings.Join(newsTitles(t, cfg), ","); got != "Fresh,Older" {
		t.Errorf("news after deleting #1 (Old) = %s, want Fresh,Older", got)
	}
}

func TestNewsEditHitsChosenItemAfterConcurrentAdd(t *testing.T) {
	cfg := staleTestConfig(t)
	seedNewsItems(t, cfg, NewsItem{ID: 1, Title: "Old", Body: "b"}, NewsItem{ID: 2, Title: "Older", Body: "b"})

	const marker = "News #1"
	ts := runStaleScreen(t, cfg, "E\r1\rT\rRenamed\rQ\rQ\r", func(ts *testSession) {
		ts.whenOutput(marker, func() { prependNews(t, cfg, "Fresh") })
	}, runEditNews)
	mustHookFire(t, ts, marker)

	if got := strings.Join(newsTitles(t, cfg), ","); got != "Fresh,Renamed,Older" {
		t.Errorf("news after editing #1 (Old) = %s, want Fresh,Renamed,Older", got)
	}
}

func TestNewsDeleteReportsItemGone(t *testing.T) {
	cfg := staleTestConfig(t)
	seedNewsItems(t, cfg, NewsItem{ID: 1, Title: "Old", Body: "b"}, NewsItem{ID: 2, Title: "Older", Body: "b"})

	const marker = "Delete #1 ("
	ts := runStaleScreen(t, cfg, "D\r1\rY\rQ\r", func(ts *testSession) {
		ts.whenOutput(marker, func() {
			// Another sysop deletes "Old" first.
			seedNewsItems(t, cfg, NewsItem{ID: 2, Title: "Older", Body: "b"})
		})
	}, runEditNews)
	mustHookFire(t, ts, marker)

	if got := strings.Join(newsTitles(t, cfg), ","); got != "Older" {
		t.Errorf("news = %s, want Older left alone", got)
	}
	if !strings.Contains(ts.output(), "no longer exists") {
		t.Errorf("expected a 'no longer exists' message, got %q", ts.output())
	}
}

// --- BBS list (#452) -------------------------------------------------------

func seedBBSListings(t *testing.T, cfg string, names ...string) {
	t.Helper()
	bld := &bbsListData{NextID: 1}
	for _, n := range names {
		bld.Listings = append(bld.Listings, BBSListing{ID: bld.NextID, Name: n, Address: strings.ToLower(n) + ".example"})
		bld.NextID++
	}
	if err := saveBBSListData(cfg, bld); err != nil {
		t.Fatal(err)
	}
}

// appendBBSListing plays another session adding a listing.
func appendBBSListing(t *testing.T, cfg, name string) {
	bbsListMu.Lock()
	defer bbsListMu.Unlock()
	bld, err := loadBBSListData(cfg)
	if err != nil {
		t.Error(err)
		return
	}
	bld.Listings = append(bld.Listings, BBSListing{ID: bld.NextID, Name: name})
	bld.NextID++
	if err := saveBBSListData(cfg, bld); err != nil {
		t.Error(err)
	}
}

func bbsListings(t *testing.T, cfg string) map[string]BBSListing {
	t.Helper()
	bld, err := loadBBSListData(cfg)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]BBSListing{}
	for _, l := range bld.Listings {
		out[l.Name] = l
	}
	return out
}

func TestBBSListDeleteKeepsConcurrentAdd(t *testing.T) {
	cfg := staleTestConfig(t)
	seedBBSListings(t, cfg, "Alpha", "Beta")

	const marker = "Delete which entry"
	ts := runStaleScreen(t, cfg, "1\rY", func(ts *testSession) {
		ts.whenOutput(marker, func() { appendBBSListing(t, cfg, "Gamma") })
	}, runBBSListDelete)
	mustHookFire(t, ts, marker)

	got := bbsListings(t, cfg)
	if _, ok := got["Alpha"]; ok {
		t.Errorf("Alpha was not deleted: %v", got)
	}
	if _, ok := got["Beta"]; !ok {
		t.Errorf("Beta was lost: %v", got)
	}
	if _, ok := got["Gamma"]; !ok {
		t.Errorf("Gamma, added by another session, was dropped: %v", got)
	}
}

func TestBBSListDeleteReportsEntryGone(t *testing.T) {
	cfg := staleTestConfig(t)
	seedBBSListings(t, cfg, "Alpha", "Beta")

	const marker = "Delete which entry"
	ts := runStaleScreen(t, cfg, "1\rY", func(ts *testSession) {
		ts.whenOutput(marker, func() {
			// Another sysop deletes Alpha and adds Gamma.
			bld := &bbsListData{NextID: 4, Listings: []BBSListing{{ID: 2, Name: "Beta"}, {ID: 3, Name: "Gamma"}}}
			if err := saveBBSListData(cfg, bld); err != nil {
				t.Error(err)
			}
		})
	}, runBBSListDelete)
	mustHookFire(t, ts, marker)

	if got := bbsListings(t, cfg); len(got) != 2 {
		t.Errorf("listings = %v, want Beta and Gamma untouched", got)
	}
	if !strings.Contains(ts.output(), "no longer exists") {
		t.Errorf("expected a 'no longer exists' message, got %q", ts.output())
	}
}

func TestBBSListVerifyKeepsConcurrentAdd(t *testing.T) {
	cfg := staleTestConfig(t)
	seedBBSListings(t, cfg, "Alpha", "Beta")

	const marker = "Toggle verified on entry"
	ts := runStaleScreen(t, cfg, "2\r", func(ts *testSession) {
		ts.whenOutput(marker, func() { appendBBSListing(t, cfg, "Gamma") })
	}, runBBSListVerify)
	mustHookFire(t, ts, marker)

	got := bbsListings(t, cfg)
	if !got["Beta"].Verified {
		t.Errorf("Beta not verified: %v", got)
	}
	if got["Alpha"].Verified {
		t.Errorf("Alpha verified by mistake: %v", got)
	}
	if _, ok := got["Gamma"]; !ok {
		t.Errorf("Gamma, added by another session, was dropped: %v", got)
	}
}

// --- want list and NUV queue (same pattern) --------------------------------

func TestWantListDeleteHitsChosenEntryAfterConcurrentDelete(t *testing.T) {
	cfg := staleTestConfig(t)
	entries := []WantListEntry{
		{Handle: "a", Filename: "one.zip"},
		{Handle: "b", Filename: "two.zip"},
		{Handle: "c", Filename: "three.zip"},
	}
	if err := saveWantList(cfg, entries); err != nil {
		t.Fatal(err)
	}

	const marker = "Entry # to delete"
	ts := runStaleScreen(t, cfg, "D\r2\r", func(ts *testSession) {
		ts.whenOutput(marker, func() {
			// Another sysop deletes entry 1 first.
			if err := saveWantList(cfg, entries[1:]); err != nil {
				t.Error(err)
			}
		})
	}, runWantList)
	mustHookFire(t, ts, marker)

	got, err := loadWantList(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Filename != "three.zip" {
		t.Errorf("want list = %v, want only three.zip (two.zip was chosen for deletion)", got)
	}
}

func TestNUVRemoveHitsChosenCandidateAfterConcurrentChange(t *testing.T) {
	cfg := staleTestConfig(t)
	nd := &NUVData{Candidates: []NUVCandidate{{Handle: "one"}, {Handle: "two"}, {Handle: "three"}}}
	if err := saveNUVData(cfg, nd); err != nil {
		t.Fatal(err)
	}

	const marker = "Remove candidate #"
	ts := runStaleScreen(t, cfg, "R2\r", func(ts *testSession) {
		ts.whenOutput(marker, func() {
			// A vote elsewhere resolves "one" and drops it from the queue.
			if err := saveNUVData(cfg, &NUVData{Candidates: nd.Candidates[1:]}); err != nil {
				t.Error(err)
			}
		})
	}, runNUVList)
	mustHookFire(t, ts, marker)

	got, err := loadNUVData(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Candidates) != 1 || got.Candidates[0].Handle != "three" {
		t.Errorf("queue = %v, want only three (two was chosen for removal)", got.Candidates)
	}
}

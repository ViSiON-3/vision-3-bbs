package menu

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// These tests cover file-backed features that let a sysop pick a record from a
// listing and then act on it after one or more prompts. Another node can change
// the same JSON file in between; the action must land on the record the sysop
// picked, keep the other node's change, and say so when the record is gone.
// "Another session" is simulated with testSession.whenOutput, which writes the
// file after a prompt is shown and before its reply is acted on.

// staleTestDataDir returns a data directory (ServerConfig.DataDir) inside the
// test's temp dir.
func staleTestDataDir(t *testing.T) string {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dataDir
}

// runStaleScreen drives fn as a sysop with the scripted input. setup may
// register output hooks on the session before fn runs.
func runStaleScreen(t *testing.T, dataDir, input string, setup func(ts *testSession), fn RunnableFunc) *testSession {
	t.Helper()
	e := &MenuExecutor{}
	e.SetServerConfig(config.ServerConfig{DataDir: dataDir})
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

func voteTopicQuestions(t *testing.T, dataDir string) map[string]int {
	t.Helper()
	vd, err := loadVotingData(dataDir)
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
	dataDir := staleTestDataDir(t)
	if err := saveVotingData(dataDir, &VotingData{Topics: []VoteTopic{
		{ID: 1, Question: "A?", Options: []string{"x"}, Votes: map[string][]string{}},
		{ID: 2, Question: "B?", Options: []string{"y"}, Votes: map[string][]string{}},
	}}); err != nil {
		t.Fatal(err)
	}

	// Delete A, add C, select topic 2 (C), delete it, quit.
	runStaleScreen(t, dataDir, "D\rY\rA\rC?\rN\rN\rz\r\r2\rD\rY\rQ\r", nil, runVote)

	got := voteTopicQuestions(t, dataDir)
	if _, ok := got["B?"]; !ok || len(got) != 1 {
		t.Fatalf("topics after deleting C = %v, want only B?", got)
	}
}

func TestVoteTopicIDAllocatorIsMonotonic(t *testing.T) {
	vd := &VotingData{Topics: []VoteTopic{{ID: 2}, {ID: 7}, {ID: 3}}}
	if got := allocVoteTopicID(vd); got != 8 {
		t.Errorf("allocVoteTopicID = %d, want 8 (one past the highest live ID)", got)
	}
	vd = &VotingData{NextID: 20, Topics: []VoteTopic{{ID: 2}}}
	if got := allocVoteTopicID(vd); got != 20 || vd.NextID != 21 {
		t.Errorf("allocVoteTopicID = %d, NextID = %d; want 20 and 21", got, vd.NextID)
	}
	if got := allocVoteTopicID(&VotingData{}); got != 1 {
		t.Errorf("allocVoteTopicID(empty) = %d, want 1", got)
	}
}

// Deleting the highest-numbered topic must not free its ID: a session that
// listed the old topic would otherwise act on the new one. The file here has
// no next_id, as voting.json files written before the allocator existed.
func TestVoteDeleteHighestThenCreateDoesNotReuseID(t *testing.T) {
	dataDir := staleTestDataDir(t)
	raw := `{"topics":[
		{"id":1,"question":"A?","options":["x"],"votes":{}},
		{"id":2,"question":"B?","options":["y"],"votes":{}}
	]}`
	if err := os.WriteFile(votingFilePath(dataDir), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	// Select B (ID 2, the highest), delete it, then add C.
	runStaleScreen(t, dataDir, "2\rD\rY\rA\rC?\rN\rN\rz\r\rQ\r", nil, runVote)

	got := voteTopicQuestions(t, dataDir)
	if _, ok := got["B?"]; ok {
		t.Fatalf("B? was not deleted: %v", got)
	}
	id, ok := got["C?"]
	if !ok {
		t.Fatalf("C? was not created: %v", got)
	}
	if id == 2 {
		t.Errorf("C? reused deleted topic B?'s ID 2; a stale session's vote or delete for B? would hit C?")
	}
	// A session still holding B?'s ID must find it gone.
	if _, err := voteRecordVote(dataDir, 2, 0, "Stale"); !errors.Is(err, errVoteTopicGone) {
		t.Errorf("vote on deleted topic ID 2: err = %v, want errVoteTopicGone", err)
	}
}

// voting.json files written by the old allocator may already hold duplicate
// IDs. Loading must give each topic its own ID without moving any votes.
func TestLoadVotingDataRepairsDuplicateIDs(t *testing.T) {
	dataDir := staleTestDataDir(t)
	raw := `{"topics":[
		{"id":2,"question":"B?","options":["y","n"],"votes":{"0":["alice"]}},
		{"id":2,"question":"C?","options":["y","n"],"votes":{"1":["bob"]}},
		{"id":0,"question":"D?","options":["y"],"votes":null},
		{"id":1,"question":"E?","options":["y"],"votes":{}}
	]}`
	if err := os.WriteFile(votingFilePath(dataDir), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	vd, err := loadVotingData(dataDir)
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
	again, err := loadVotingData(dataDir)
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
	dataDir := staleTestDataDir(t)
	topics := []VoteTopic{
		{ID: 1, Question: "A?", Options: []string{"x"}, Votes: map[string][]string{}},
		{ID: 2, Question: "B?", Options: []string{"y"}, Votes: map[string][]string{}},
		{ID: 3, Question: "C?", Options: []string{"z"}, Votes: map[string][]string{}},
	}
	if err := saveVotingData(dataDir, &VotingData{Topics: topics}); err != nil {
		t.Fatal(err)
	}
	const marker = "Your selection"
	ts := runStaleScreen(t, dataDir, "2\rV\r1\r\rQ\r", func(ts *testSession) {
		ts.whenOutput(marker, func() {
			// Another sysop deletes A while this caller is choosing.
			if err := saveVotingData(dataDir, &VotingData{Topics: topics[1:]}); err != nil {
				t.Error(err)
			}
		})
	}, runVote)
	mustHookFire(t, ts, marker)

	vd, err := loadVotingData(dataDir)
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

func seedNewsItems(t *testing.T, dataDir string, items ...NewsItem) {
	t.Helper()
	nd := &NewsData{Items: items}
	for _, it := range items {
		if it.ID >= nd.NextID {
			nd.NextID = it.ID + 1
		}
	}
	if err := saveNewsData(dataDir, nd); err != nil {
		t.Fatal(err)
	}
}

func newsTitles(t *testing.T, dataDir string) []string {
	t.Helper()
	nd, err := loadNewsData(dataDir)
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
func prependNews(t *testing.T, dataDir, title string) {
	newsMu.Lock()
	defer newsMu.Unlock()
	nd, err := loadNewsData(dataDir)
	if err != nil {
		t.Error(err)
		return
	}
	nd.Items = append([]NewsItem{{ID: allocNewsID(nd), Title: title, Body: "b"}}, nd.Items...)
	if err := saveNewsData(dataDir, nd); err != nil {
		t.Error(err)
	}
}

func TestNewsDeleteHitsChosenItemAfterConcurrentAdd(t *testing.T) {
	dataDir := staleTestDataDir(t)
	seedNewsItems(t, dataDir, NewsItem{ID: 1, Title: "Old", Body: "b"}, NewsItem{ID: 2, Title: "Older", Body: "b"})

	const marker = "Delete #1 ("
	ts := runStaleScreen(t, dataDir, "D\r1\rY\rQ\r", func(ts *testSession) {
		ts.whenOutput(marker, func() { prependNews(t, dataDir, "Fresh") })
	}, runEditNews)
	mustHookFire(t, ts, marker)

	if got := strings.Join(newsTitles(t, dataDir), ","); got != "Fresh,Older" {
		t.Errorf("news after deleting #1 (Old) = %s, want Fresh,Older", got)
	}
}

func TestNewsEditHitsChosenItemAfterConcurrentAdd(t *testing.T) {
	dataDir := staleTestDataDir(t)
	seedNewsItems(t, dataDir, NewsItem{ID: 1, Title: "Old", Body: "b"}, NewsItem{ID: 2, Title: "Older", Body: "b"})

	const marker = "News #1"
	ts := runStaleScreen(t, dataDir, "E\r1\rT\rRenamed\rQ\rQ\r", func(ts *testSession) {
		ts.whenOutput(marker, func() { prependNews(t, dataDir, "Fresh") })
	}, runEditNews)
	mustHookFire(t, ts, marker)

	if got := strings.Join(newsTitles(t, dataDir), ","); got != "Fresh,Renamed,Older" {
		t.Errorf("news after editing #1 (Old) = %s, want Fresh,Renamed,Older", got)
	}
}

// A news.json written before next_id existed: deleting the highest-numbered
// item through the editor must still record that its ID was used, so the
// next add cannot hand it out again (users' seen-sets are keyed by ID).
func TestNewsDeleteHighestThenAddDoesNotReuseID(t *testing.T) {
	dataDir := staleTestDataDir(t)
	raw := `{"items":[
		{"id":2,"title":"New","body":"b"},
		{"id":1,"title":"Old","body":"b"}
	]}`
	if err := os.WriteFile(newsFilePath(dataDir), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	// Delete #1 (New, ID 2), then add "Fresh" with defaults and one body line.
	runStaleScreen(t, dataDir, "D\r1\rY\rA\rFresh\r\r\r\r\rhello\r\rQ\r", nil, runEditNews)

	nd, err := loadNewsData(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	var fresh *NewsItem
	for i := range nd.Items {
		switch nd.Items[i].Title {
		case "New":
			t.Fatalf("New was not deleted: %v", nd.Items)
		case "Fresh":
			fresh = &nd.Items[i]
		}
	}
	if fresh == nil {
		t.Fatalf("Fresh was not added: %v", nd.Items)
	}
	if fresh.ID == 2 {
		t.Errorf("Fresh reused deleted item New's ID 2")
	}
}

func TestNewsDeleteReportsItemGone(t *testing.T) {
	dataDir := staleTestDataDir(t)
	seedNewsItems(t, dataDir, NewsItem{ID: 1, Title: "Old", Body: "b"}, NewsItem{ID: 2, Title: "Older", Body: "b"})

	const marker = "Delete #1 ("
	ts := runStaleScreen(t, dataDir, "D\r1\rY\rQ\r", func(ts *testSession) {
		ts.whenOutput(marker, func() {
			// Another sysop deletes "Old" first.
			seedNewsItems(t, dataDir, NewsItem{ID: 2, Title: "Older", Body: "b"})
		})
	}, runEditNews)
	mustHookFire(t, ts, marker)

	if got := strings.Join(newsTitles(t, dataDir), ","); got != "Older" {
		t.Errorf("news = %s, want Older left alone", got)
	}
	if !strings.Contains(ts.output(), "no longer exists") {
		t.Errorf("expected a 'no longer exists' message, got %q", ts.output())
	}
}

// --- BBS list (#452) -------------------------------------------------------

func seedBBSListings(t *testing.T, dataDir string, names ...string) {
	t.Helper()
	bld := &bbsListData{NextID: 1}
	for _, n := range names {
		bld.Listings = append(bld.Listings, BBSListing{ID: bld.NextID, Name: n, Address: strings.ToLower(n) + ".example"})
		bld.NextID++
	}
	if err := saveBBSListData(dataDir, bld); err != nil {
		t.Fatal(err)
	}
}

// appendBBSListing plays another session adding a listing.
func appendBBSListing(t *testing.T, dataDir, name string) {
	bbsListMu.Lock()
	defer bbsListMu.Unlock()
	bld, err := loadBBSListData(dataDir)
	if err != nil {
		t.Error(err)
		return
	}
	bld.Listings = append(bld.Listings, BBSListing{ID: bld.NextID, Name: name})
	bld.NextID++
	if err := saveBBSListData(dataDir, bld); err != nil {
		t.Error(err)
	}
}

func bbsListings(t *testing.T, dataDir string) map[string]BBSListing {
	t.Helper()
	bld, err := loadBBSListData(dataDir)
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
	dataDir := staleTestDataDir(t)
	seedBBSListings(t, dataDir, "Alpha", "Beta")

	const marker = "Delete which entry"
	ts := runStaleScreen(t, dataDir, "1\rY", func(ts *testSession) {
		ts.whenOutput(marker, func() { appendBBSListing(t, dataDir, "Gamma") })
	}, runBBSListDelete)
	mustHookFire(t, ts, marker)

	got := bbsListings(t, dataDir)
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
	dataDir := staleTestDataDir(t)
	seedBBSListings(t, dataDir, "Alpha", "Beta")

	const marker = "Delete which entry"
	ts := runStaleScreen(t, dataDir, "1\rY", func(ts *testSession) {
		ts.whenOutput(marker, func() {
			// Another sysop deletes Alpha and adds Gamma.
			bld := &bbsListData{NextID: 4, Listings: []BBSListing{{ID: 2, Name: "Beta"}, {ID: 3, Name: "Gamma"}}}
			if err := saveBBSListData(dataDir, bld); err != nil {
				t.Error(err)
			}
		})
	}, runBBSListDelete)
	mustHookFire(t, ts, marker)

	if got := bbsListings(t, dataDir); len(got) != 2 {
		t.Errorf("listings = %v, want Beta and Gamma untouched", got)
	}
	if !strings.Contains(ts.output(), "no longer exists") {
		t.Errorf("expected a 'no longer exists' message, got %q", ts.output())
	}
}

func TestBBSListVerifyKeepsConcurrentAdd(t *testing.T) {
	dataDir := staleTestDataDir(t)
	seedBBSListings(t, dataDir, "Alpha", "Beta")

	const marker = "Toggle verified on entry"
	ts := runStaleScreen(t, dataDir, "2\r", func(ts *testSession) {
		ts.whenOutput(marker, func() { appendBBSListing(t, dataDir, "Gamma") })
	}, runBBSListVerify)
	mustHookFire(t, ts, marker)

	got := bbsListings(t, dataDir)
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
	dataDir := staleTestDataDir(t)
	entries := []WantListEntry{
		{ID: 1, Handle: "a", Filename: "one.zip"},
		{ID: 2, Handle: "b", Filename: "two.zip"},
		{ID: 3, Handle: "c", Filename: "three.zip"},
	}
	if err := saveWantList(dataDir, &wantListData{Entries: entries, NextID: 4}); err != nil {
		t.Fatal(err)
	}

	const marker = "Entry # to delete"
	ts := runStaleScreen(t, dataDir, "D\r2\r", func(ts *testSession) {
		ts.whenOutput(marker, func() {
			// Another sysop deletes entry 1 first.
			if err := saveWantList(dataDir, &wantListData{Entries: entries[1:], NextID: 4}); err != nil {
				t.Error(err)
			}
		})
	}, runWantList)
	mustHookFire(t, ts, marker)

	got, err := loadWantList(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Filename != "three.zip" {
		t.Errorf("want list = %v, want only three.zip (two.zip was chosen for deletion)", got.Entries)
	}
}

// Two identical requests are allowed. When the sysop picks the second and
// another session deletes that same one first, the first must survive:
// matching by content would find it and delete it instead.
func TestWantListDeleteIdenticalEntriesByID(t *testing.T) {
	dataDir := staleTestDataDir(t)
	dup := WantListEntry{Handle: "a", Filename: "same.zip", Reason: "r", Date: "01/01/2026"}
	first, second := dup, dup
	first.ID, second.ID = 1, 2
	other := WantListEntry{ID: 3, Handle: "c", Filename: "other.zip"}
	if err := saveWantList(dataDir, &wantListData{Entries: []WantListEntry{first, second, other}, NextID: 4}); err != nil {
		t.Fatal(err)
	}

	const marker = "Entry # to delete"
	ts := runStaleScreen(t, dataDir, "D\r2\r", func(ts *testSession) {
		ts.whenOutput(marker, func() {
			if err := saveWantList(dataDir, &wantListData{Entries: []WantListEntry{first, other}, NextID: 4}); err != nil {
				t.Error(err)
			}
		})
	}, runWantList)
	mustHookFire(t, ts, marker)

	got, err := loadWantList(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 || got.Entries[0].ID != 1 || got.Entries[1].ID != 3 {
		t.Errorf("want list = %v, want entries 1 and 3 kept (entry 2 was already gone)", got.Entries)
	}
	if !strings.Contains(ts.output(), "no longer exists") {
		t.Errorf("expected a 'no longer exists' message, got %q", ts.output())
	}
}

// A legacy wantlist.json is a bare array without IDs. Loading it assigns IDs
// in list order, the same on every load, and the allocator starts after them.
func TestLoadWantListMigratesLegacyArray(t *testing.T) {
	dataDir := staleTestDataDir(t)
	raw := `[{"handle":"a","filename":"x.zip"},{"handle":"a","filename":"x.zip"},{"handle":"b","filename":"y.zip"}]`
	if err := os.WriteFile(wantListFilePath(dataDir), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		wl, err := loadWantList(dataDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(wl.Entries) != 3 || wl.Entries[0].ID != 1 || wl.Entries[1].ID != 2 || wl.Entries[2].ID != 3 || wl.NextID != 4 {
			t.Fatalf("loaded %v NextID=%d, want IDs 1,2,3 and NextID 4", wl.Entries, wl.NextID)
		}
	}
}

func TestNUVRemoveHitsChosenCandidateAfterConcurrentChange(t *testing.T) {
	dataDir := staleTestDataDir(t)
	nd := &NUVData{Candidates: []NUVCandidate{{Handle: "one"}, {Handle: "two"}, {Handle: "three"}}}
	if err := saveNUVData(dataDir, nd); err != nil {
		t.Fatal(err)
	}

	const marker = "Remove candidate #"
	ts := runStaleScreen(t, dataDir, "R2\r", func(ts *testSession) {
		ts.whenOutput(marker, func() {
			// A vote elsewhere resolves "one" and drops it from the queue.
			if err := saveNUVData(dataDir, &NUVData{Candidates: nd.Candidates[1:]}); err != nil {
				t.Error(err)
			}
		})
	}, runNUVList)
	mustHookFire(t, ts, marker)

	got, err := loadNUVData(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Candidates) != 1 || got.Candidates[0].Handle != "three" {
		t.Errorf("queue = %v, want only three (two was chosen for removal)", got.Candidates)
	}
}

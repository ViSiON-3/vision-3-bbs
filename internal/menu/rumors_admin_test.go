package menu

import (
	"slices"
	"testing"
	"time"
)

// A caller below the anonymous level is not offered anonymity, and entering
// no text saves nothing.
func TestRumorsAddBlankTextSavesNothing(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("RUMORSADD", env.caller, "", "\r\r")
	if r.err != nil || r.user != env.caller {
		t.Fatalf("err=%v user=%v", r.err, r.user)
	}
	if r.has("Anonymous?") {
		t.Errorf("level-10 caller was offered anonymity (anonymousLevel is 50):\n%s", r.text())
	}
	if !r.has("Enter Rumor") || r.has("Rumor has been added!") {
		t.Errorf("blank rumor flow:\n%s", r.text())
	}
	if rd := loadEnvRumors(t, env); len(rd.Rumors) != 0 {
		t.Errorf("blank rumor was saved: %+v", rd.Rumors)
	}
}

// Levels 0 and 1 may not post rumors at all.
func TestRumorsAddRefusesLevelBelowTwo(t *testing.T) {
	env := newMenuEnv(t)
	low := *env.caller
	low.AccessLevel = 1
	r := env.runCmd("RUMORSADD", &low, "", "\r\rsneaky\r")
	if !r.has("You need at least level 2 to add rumors.") {
		t.Errorf("low-level add:\n%s", r.text())
	}
	if rd := loadEnvRumors(t, env); len(rd.Rumors) != 0 {
		t.Errorf("low-level caller saved a rumor: %+v", rd.Rumors)
	}
}

// The board holds at most 999 rumors; a full board refuses new ones before
// prompting for anything.
func TestRumorsAddRefusesWhenFull(t *testing.T) {
	env := newMenuEnv(t)
	rd := &rumorsData{NextID: 1000}
	for i := 1; i <= 999; i++ {
		rd.Rumors = append(rd.Rumors, RumorRecord{ID: i, Text: "x", MinLevel: 1})
	}
	seedRumors(t, env, rd)

	r := env.runCmd("RUMORSADD", env.caller, "", "\rone too many\r")
	if !r.has("too many rumors") || r.has("Enter Rumor") {
		t.Errorf("full board:\n%s", r.text())
	}
	if got := len(loadEnvRumors(t, env).Rumors); got != 999 {
		t.Errorf("rumor count = %d, want 999", got)
	}
}

// A sysop posting anonymously: out-of-range levels are re-asked, the rumor
// is stored under the anonymous name with the real poster and ID kept for
// ownership, pipe codes are defused, and the ID comes from NextID.
func TestRumorsAddAnonymousWithLevel(t *testing.T) {
	env := newMenuEnv(t)
	seedRumors(t, env, &rumorsData{NextID: 7})

	before := time.Now().UTC().Add(-time.Second)
	r := env.runCmd("RUMORSADD", env.sysop, "", "Y0\rabc\r300\r50\r  |04red alert  \r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Anonymous?", "Invalid level. Enter a number from 1-255.", "Rumor has been added!") {
		t.Errorf("add flow:\n%s", r.text())
	}

	rd := loadEnvRumors(t, env)
	if len(rd.Rumors) != 1 {
		t.Fatalf("rumors = %+v, want one", rd.Rumors)
	}
	got := rd.Rumors[0]
	if got.ID != 7 || rd.NextID != 8 {
		t.Errorf("ID=%d NextID=%d, want 7 and 8", got.ID, rd.NextID)
	}
	if got.Author != rumorAnonName(env.e) || got.RealUser != "Sysop" || got.UserID != 1 {
		t.Errorf("author fields = %q/%q/%d", got.Author, got.RealUser, got.UserID)
	}
	if got.MinLevel != 50 {
		t.Errorf("MinLevel = %d, want 50 (invalid entries re-asked)", got.MinLevel)
	}
	if got.Text != "\xc2\xa604red alert" {
		t.Errorf("Text = %q, want trimmed with the pipe defused", got.Text)
	}
	if got.PostedAt.Before(before) {
		t.Errorf("PostedAt = %v, want now", got.PostedAt)
	}
}

// deleteFixture has one rumor by the sysop, one by the caller, one legacy
// caller rumor with no UserID, and one only level 100+ may see.
func deleteFixture() *rumorsData {
	return &rumorsData{NextID: 5, Rumors: []RumorRecord{
		{ID: 1, Author: "Sysop", RealUser: "Sysop", UserID: 1, Text: "sysop rumor", MinLevel: 1},
		{ID: 2, Author: "Caller", RealUser: "Caller", UserID: 2, Text: "caller rumor", MinLevel: 1},
		{ID: 3, Author: "Caller", RealUser: "caller", Text: "legacy caller rumor", MinLevel: 1},
		{ID: 4, Author: "Caller", RealUser: "Caller", UserID: 2, Text: "high rumor", MinLevel: 100},
	}}
}

// rumorIDs lists the IDs left on disk.
func rumorIDs(t *testing.T, env *menuEnv) []int {
	t.Helper()
	var ids []int
	for _, r := range loadEnvRumors(t, env).Rumors {
		ids = append(ids, r.ID)
	}
	return ids
}

// A caller may delete their own rumor after confirming, via the ? list.
func TestRumorsDeleteOwnRumor(t *testing.T) {
	env := newMenuEnv(t)
	seedRumors(t, env, deleteFixture())

	r := env.runCmd("RUMORSDELETE", env.caller, "", "?\r2\rY")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("sysop rumor", "caller rumor", "Delete this rumor?", "Rumor deleted.") {
		t.Errorf("delete flow:\n%s", r.text())
	}
	if r.has("high rumor") {
		t.Errorf("? list showed a rumor above the caller's level")
	}
	if ids := rumorIDs(t, env); !slices.Equal(ids, []int{1, 3, 4}) {
		t.Errorf("IDs left = %v, want [1 3 4]", ids)
	}
}

// Answering No to the confirmation keeps the rumor.
func TestRumorsDeleteDeclined(t *testing.T) {
	env := newMenuEnv(t)
	seedRumors(t, env, deleteFixture())
	r := env.runCmd("RUMORSDELETE", env.caller, "", "2\rN")
	if r.has("Rumor deleted.") {
		t.Errorf("declined delete reported success:\n%s", r.text())
	}
	if ids := rumorIDs(t, env); len(ids) != 4 {
		t.Errorf("IDs left = %v, want all four", ids)
	}
}

// A caller cannot delete someone else's rumor, nor one above their level
// (which reads as not found so its existence is not leaked), nor an ID that
// does not exist or is not a number.
func TestRumorsDeleteRefusals(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"not owner", "1\rY", "You didn't post that!"},
		{"above level", "4\rY", "Rumor not found."},
		{"no such id", "99\rY", "Rumor not found."},
		{"not a number", "two\r", "Invalid number."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newMenuEnv(t)
			seedRumors(t, env, deleteFixture())
			r := env.runCmd("RUMORSDELETE", env.caller, "", tc.input)
			if !r.has(tc.want) || r.has("Rumor deleted.") {
				t.Errorf("want %q:\n%s", tc.want, r.text())
			}
			if ids := rumorIDs(t, env); len(ids) != 4 {
				t.Errorf("IDs left = %v, want all four", ids)
			}
		})
	}
}

// A legacy rumor without a UserID is claimed by matching its RealUser handle
// (case-insensitively), and the back-filled UserID is written to disk.
func TestRumorsDeleteLegacyOwnerBackfill(t *testing.T) {
	env := newMenuEnv(t)
	seedRumors(t, env, deleteFixture())

	// Blank answer: nothing deleted, but the migration still ran.
	env.runCmd("RUMORSDELETE", env.caller, "", "\r")
	rd := loadEnvRumors(t, env)
	if rd.Rumors[2].UserID != 2 {
		t.Errorf("legacy rumor UserID = %d, want back-filled 2", rd.Rumors[2].UserID)
	}

	r := env.runCmd("RUMORSDELETE", env.caller, "", "3\rY")
	if !r.has("Rumor deleted.") {
		t.Errorf("legacy owner delete:\n%s", r.text())
	}
	if ids := rumorIDs(t, env); !slices.Equal(ids, []int{1, 2, 4}) {
		t.Errorf("IDs left = %v, want [1 2 4]", ids)
	}
}

// The sysop may delete any rumor, including ones they cannot own.
func TestRumorsDeleteSysopDeletesAnyone(t *testing.T) {
	env := newMenuEnv(t)
	seedRumors(t, env, deleteFixture())
	r := env.runCmd("RUMORSDELETE", env.sysop, "", "4\rY")
	if !r.has("high rumor", "Rumor deleted.") {
		t.Errorf("sysop delete:\n%s", r.text())
	}
	if ids := rumorIDs(t, env); !slices.Equal(ids, []int{1, 2, 3}) {
		t.Errorf("IDs left = %v, want [1 2 3]", ids)
	}
}

// With nothing visible to delete the handler says so without prompting, and
// a disconnect at the confirmation logs the caller off rather than reading
// as No.
func TestRumorsDeleteEmptyAndDisconnect(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("RUMORSDELETE", env.caller, "", "1\r")
	if !r.has("No rumors to delete.") || r.has("Rumor number") {
		t.Errorf("empty board:\n%s", r.text())
	}

	seedRumors(t, env, deleteFixture())
	r = env.runCmd("RUMORSDELETE", env.caller, "", "2\r")
	if r.next != "LOGOFF" || r.user != nil {
		t.Errorf("disconnect at confirm: next=%q user=%v, want LOGOFF/nil", r.next, r.user)
	}
	if ids := rumorIDs(t, env); len(ids) != 4 {
		t.Errorf("disconnect deleted something: %v", ids)
	}

	if r := env.runCmd("RUMORSDELETE", nil, "", "1\r"); r.raw != "" {
		t.Errorf("logged-out run wrote %q", r.raw)
	}
}

package menu

import "testing"

// TestStepIndex covers the wraparound used by file (and mirrors message) area /
// conference next-prev navigation, including the "current item not found" case.
func TestStepIndex(t *testing.T) {
	tests := []struct {
		name       string
		current, n int
		forward    bool
		want       int
	}{
		{"forward mid", 1, 4, true, 2},
		{"forward wraps at end", 3, 4, true, 0},
		{"backward mid", 2, 4, false, 1},
		{"backward wraps at start", 0, 4, false, 3},
		{"not found goes first (forward)", -1, 4, true, 0},
		{"not found goes first (backward)", -1, 4, false, 0},
		{"single item forward", 0, 1, true, 0},
		{"single item backward", 0, 1, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stepIndex(tc.current, tc.n, tc.forward); got != tc.want {
				t.Errorf("stepIndex(%d,%d,%v) = %d, want %d", tc.current, tc.n, tc.forward, got, tc.want)
			}
		})
	}
}

// TestNextPrevFileAreaWrapsAndSaves pins NEXTFILEAREA/PREVFILEAREA: from no
// area the sysop lands on General Files, steps to the Upload Queue, wraps
// back, and PREV wraps to the last; every step is saved.
func TestNextPrevFileAreaWrapsAndSaves(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.CurrentFileConferenceID = 1

	for i, st := range []struct{ cmd, want string }{
		{"NEXTFILEAREA", "GENERAL"},
		{"NEXTFILEAREA", "UPLOADS"},
		{"NEXTFILEAREA", "GENERAL"},
		{"PREVFILEAREA", "UPLOADS"},
	} {
		r := env.runCmd(st.cmd, env.sysop, "", "")
		if r.err != nil {
			t.Fatalf("step %d %s: err = %v", i, st.cmd, r.err)
		}
		if env.sysop.CurrentFileAreaTag != st.want {
			t.Fatalf("step %d %s: area = %q, want %q", i, st.cmd, env.sysop.CurrentFileAreaTag, st.want)
		}
		if !r.has("(" + st.want + ")") {
			t.Errorf("step %d: missing current-area notice:\n%s", i, r.text())
		}
		if saved := env.mustDiskUser(env.sysop.ID); saved.CurrentFileAreaTag != st.want {
			t.Errorf("step %d: saved area = %q, want %q", i, saved.CurrentFileAreaTag, st.want)
		}
	}
}

// TestNextFileAreaSkipsUnlistableAreas pins that the caller, who may not
// list the Upload Queue, stays on General Files in both directions.
func TestNextFileAreaSkipsUnlistableAreas(t *testing.T) {
	env := newMenuEnv(t)
	env.caller.CurrentFileConferenceID = 1
	env.caller.CurrentFileAreaID, env.caller.CurrentFileAreaTag = 1, "GENERAL"

	for _, cmd := range []string{"NEXTFILEAREA", "PREVFILEAREA"} {
		env.runCmd(cmd, env.caller, "", "")
		if env.caller.CurrentFileAreaTag != "GENERAL" {
			t.Errorf("%s moved the caller to %q, which it cannot list", cmd, env.caller.CurrentFileAreaTag)
		}
	}
}

// TestNextPrevFileConfJoinsBothMenus pins NEXTFILECONF/PREVFILECONF: the move
// joins the conference for messages as well as files (#304), clearing areas
// in the empty FelonyNet and landing on Local's first areas on the way back.
func TestNextPrevFileConfJoinsBothMenus(t *testing.T) {
	env := newMenuEnv(t)
	u := env.sysop
	u.CurrentFileConferenceID, u.CurrentMsgConferenceID = 1, 1
	u.CurrentFileAreaID, u.CurrentMessageAreaID = 2, 2

	r := env.runCmd("NEXTFILECONF", u, "", "")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if u.CurrentFileConferenceTag != "FELONYNET" || u.CurrentMsgConferenceID != 2 {
		t.Fatalf("after NEXT: file conf %q, msg conf %d; want FELONYNET/2", u.CurrentFileConferenceTag, u.CurrentMsgConferenceID)
	}
	if u.CurrentFileAreaID != 0 || u.CurrentMessageAreaID != 0 {
		t.Errorf("FelonyNet has no areas; want both cleared, got file %d msg %d", u.CurrentFileAreaID, u.CurrentMessageAreaID)
	}
	if !r.has("FelonyNet") {
		t.Errorf("missing conference notice:\n%s", r.text())
	}

	env.runCmd("PREVFILECONF", u, "", "")
	saved := env.mustDiskUser(u.ID)
	if saved.CurrentFileConferenceID != 1 || saved.CurrentFileAreaTag != "GENERAL" || saved.CurrentMessageAreaTag != "GENERAL" {
		t.Errorf("saved after PREV = conf %d, file %q, msg %q; want 1/GENERAL/GENERAL",
			saved.CurrentFileConferenceID, saved.CurrentFileAreaTag, saved.CurrentMessageAreaTag)
	}
}

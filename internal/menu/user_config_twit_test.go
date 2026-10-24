package menu

import (
	"slices"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestCfgTwitListPersistenceAndRules(t *testing.T) {
	env := newMsgEnv(t)
	r := env.runCmd("CFG_TWITLIST", env.caller, "", "A\rCaller\rA\rSysop\rA\rRemote@21:1/100\rA\rRemote@21:1/100\rQ\r")
	if r.err != nil || !r.has("cannot add yourself") || !r.has("exempt") || !r.has("already on") {
		t.Fatalf("rules: %v\n%s", r.err, r.text())
	}
	got := env.mustDiskUser(2)
	if len(got.TwitList) != 1 || got.TwitList[0].Addr != "21:1/100" {
		t.Fatalf("saved: %+v", got.TwitList)
	}
	r = env.runCmd("CFG_TWITLIST", got, "", "D\r9\rC\rN\rQ\r")
	if !r.has("Invalid") || len(env.mustDiskUser(2).TwitList) != 1 {
		t.Fatal("invalid delete or declined clear modified list")
	}
	env.runCmd("CFG_TWITLIST", env.mustDiskUser(2), "", "D\r1\rA\rRemote\rC\rY\rQ\r")
	if len(env.mustDiskUser(2).TwitList) != 0 {
		t.Fatal("clear did not persist")
	}
}
func TestTwitSaveFailureRestoresSessionAndDisk(t *testing.T) {
	dir := t.TempDir()
	um, err := user.NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	u, err := um.AddUser("password", "Reader", "Real Reader", "Loc")
	if err != nil {
		t.Fatal(err)
	}
	u.TwitList = []user.TwitEntry{{Name: "Existing"}}
	if err := um.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	breakUserStore(t, dir)
	if err := saveTwitList(um, u, []user.TwitEntry{{Name: "New"}}); err == nil {
		t.Fatal("save should fail")
	}
	if u.TwitList[0].Name != "Existing" || reloadPersistedUser(t, dir, u.Handle).TwitList[0].Name != "Existing" {
		t.Fatal("failed save changed list")
	}
}
func TestTwitReaderQuickAddAndNavigation(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Remote", to: "All", subject: "visible-first"}, testMsg{from: "Remote", to: "All", subject: "hidden-second"}, testMsg{from: "Other", to: "All", subject: "visible-third"})
	r := env.runCmd("READMSGS", env.caller, "", "IYQ")
	if r.err != nil || !r.has("visible-third-body") || r.has("hidden-second-body") {
		t.Fatalf("quick-add: %v\n%s", r.err, r.text())
	}
	if got := env.mustDiskUser(2).TwitList; len(got) != 1 || got[0].Name != "Remote" {
		t.Fatalf("quick-add persistence: %+v", got)
	}
	r = env.runCmd("READMSGS", env.mustDiskUser(2), "", "J1\rSQ")
	if r.has("visible-first-body") || r.has("hidden-second-body") {
		t.Fatalf("jump/prev exposed twit: %s", r.text())
	}
}
func TestTwitListsPrivateMailAndThreads(t *testing.T) {
	env := newMsgEnv(t)
	env.caller.TwitList = []user.TwitEntry{{Name: "Remote"}}
	env.postMsgs(generalAreaID, testMsg{from: "Other", to: "All", subject: "Thread"}, testMsg{from: "Remote", to: "All", subject: "Re: Thread"}, testMsg{from: "Other", to: "All", subject: "Re: Thread"})
	matcher := twitMatcher(env.e, env.um, env.caller)
	filter := withTwit(func() *user.TwitMatcher { return matcher }, nil, nil)
	entries, _, err := buildMessageList(env.e.MessageMgr, generalAreaID, "Caller", filter)
	if err != nil || len(entries) != 2 {
		t.Fatalf("list=%+v err=%v", entries, err)
	}
	n, found := forwardBackThread(env.e, generalAreaID, 1, 3, "Thread", true, filter)
	if !found || n != 3 {
		t.Fatalf("forward thread=%d,%v", n, found)
	}
	n, found = forwardBackThread(env.e, generalAreaID, 3, 3, "Thread", false, filter)
	if !found || n != 1 {
		t.Fatalf("back thread=%d,%v", n, found)
	}
	env.postMsgs(privmailAreaID, testMsg{from: "Remote", to: "Caller", subject: "hidden-mail", private: true}, testMsg{from: "Other", to: "Caller", subject: "visible-mail", private: true})
	for _, cmd := range []string{"READPRIVMAIL", "LISTPRIVMAIL"} {
		r := env.runCmd(cmd, env.caller, "", "Q")
		if strings.Contains(r.text(), "hidden-mail") || !r.has("visible-mail") {
			t.Fatalf("%s: %v\n%s", cmd, r.err, r.text())
		}
	}
}
func TestTwitCounterRespectsOtherFiltersAndDeduplicates(t *testing.T) {
	matcher := user.BuildTwitMatcher([]user.TwitEntry{{Name: "Remote"}}, nil, user.TwitPolicy{}, "")
	count := &twitScanCounter{}
	filter := withTwit(func() *user.TwitMatcher { return matcher }, func(m *message.DisplayMessage) bool { return m.To == "Caller" }, count)
	for _, m := range []*message.DisplayMessage{{AreaID: 1, MsgNum: 1, From: "Remote", To: "Other"}, {AreaID: 1, MsgNum: 2, From: "Remote", To: "Caller"}, {AreaID: 1, MsgNum: 2, From: "Remote", To: "Caller"}, {AreaID: 2, MsgNum: 2, From: "Remote", To: "Caller"}} {
		filter(m)
	}
	if len(count.hidden) != 2 {
		t.Fatalf("hidden=%v", count.hidden)
	}
}
func TestTwitNewscanSummary(t *testing.T) {
	env := newMsgEnv(t)
	beta := addScanArea(env, "BETA", 1, 0)
	env.caller.TwitList = []user.TwitEntry{{Name: "Remote"}}
	tagAreas(env, "GENERAL", "BETA")
	env.postMsgs(generalAreaID, testMsg{from: "Remote", to: "All", subject: "hide-one"}, testMsg{from: "Other", to: "All", subject: "show-one"})
	env.postMsgs(beta, testMsg{from: "Remote", to: "All", subject: "hide-two"}, testMsg{from: "Other", to: "All", subject: "show-two"})
	r := env.runCmd("NEWSCAN", env.caller, "", "\rNNN")
	if !r.has("2 messages hidden by your Twit List") || strings.Count(r.text(), "messages hidden by your Twit List") != 1 || r.has("hide-one-body") || r.has("hide-two-body") {
		t.Fatalf("scan: %v\n%s", r.err, r.text())
	}
}
func TestTwitListCopiesAreIndependent(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	u.TwitList = []user.TwitEntry{{Name: "Original"}}
	if err := um.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	u.TwitList[0].Name = "Changed"
	stored, _ := um.GetUserByID(u.ID)
	if stored.TwitList[0].Name != "Original" {
		t.Fatal("write alias")
	}
	stored.TwitList[0].Name = "Changed Again"
	again, _ := um.GetUserByID(u.ID)
	if !slices.Equal(again.TwitList, []user.TwitEntry{{Name: "Original"}}) {
		t.Fatal("read alias")
	}
}

func TestTwitQuickAddSelfExemptAndAnonymous(t *testing.T) {
	for _, tc := range []struct{ from, keys, want string }{
		{"Caller", "IYQ", "cannot add yourself"},
		{"Sysop", "IYQ", "exempt"},
		{"Anonymous", "IQ", "Anonymous authors cannot be added"},
	} {
		t.Run(tc.from, func(t *testing.T) {
			env := newMsgEnv(t)
			env.postMsgs(generalAreaID, testMsg{from: tc.from, to: "All", subject: "test"})
			r := env.runCmd("READMSGS", env.caller, "", tc.keys)
			if !r.has(tc.want) || len(env.mustDiskUser(2).TwitList) != 0 {
				t.Fatalf("%v\n%s", r.err, r.text())
			}
		})
	}
}
func TestTwitQuickAddRealNamePreservesLocalExemption(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: env.sysop.RealName, to: "All", subject: "real-name-post"})
	r := env.runCmd("READMSGS", env.caller, "", "IYQ")
	if !r.has("exempt") || len(env.mustDiskUser(2).TwitList) != 0 {
		t.Fatalf("%v\n%s", r.err, r.text())
	}
}
func TestTwitConfiguredCap(t *testing.T) {
	env := newMsgEnv(t)
	cfg := env.e.GetServerConfig()
	cfg.TwitListMax = 1
	env.e.SetServerConfig(cfg)
	r := env.runCmd("CFG_TWITLIST", env.caller, "", "A\rFirst\rA\rSecond\rQ\r")
	if !r.has("full") || len(env.mustDiskUser(2).TwitList) != 1 {
		t.Fatalf("%v\n%s", r.err, r.text())
	}
}
func TestTwitKonfigLaunchAndReturn(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	screen, _, _ := runKonfig(t, um, u, "mA\rRemote\rQ\rq", "")
	stored, _ := um.GetUserByID(u.ID)
	if len(stored.TwitList) != 1 || stored.TwitList[0].Name != "Remote" {
		t.Fatalf("stored=%+v", stored.TwitList)
	}
	if !strings.Contains(screen.Snapshot(), "[M] Twit List") {
		t.Fatalf("form did not redraw:\n%s", screen.Snapshot())
	}
}
func TestTwitLocalEntrySurvivesRenameAndPromotion(t *testing.T) {
	env := newMsgEnv(t)
	target, err := env.um.AddUser("password", "Before", "Target Real Name", "Loc")
	if err != nil {
		t.Fatal(err)
	}
	target.AccessLevel = 30
	if err := env.um.UpdateUser(target); err != nil {
		t.Fatal(err)
	}
	env.runCmd("CFG_TWITLIST", env.caller, "", "A\rBefore\rQ\r")
	env.caller = env.mustDiskUser(2)
	if env.caller.TwitList[0].UserID != target.ID {
		t.Fatal("not a local entry")
	}
	target.Handle = "After"
	if err := env.um.UpdateUserByID(target); err != nil {
		t.Fatal(err)
	}
	matcher := twitMatcher(env.e, env.um, env.caller)
	for _, name := range []string{"Before", "After", "Target Real Name"} {
		if !matcher.Hidden(name, "") {
			t.Fatalf("rename lost %q", name)
		}
	}
	target.AccessLevel = 255
	if err := env.um.UpdateUser(target); err != nil {
		t.Fatal(err)
	}
	matcher = twitMatcher(env.e, env.um, env.caller)
	if matcher.Hidden("After", "") || matcher.Hidden("Before", "") {
		t.Fatal("promotion ignored")
	}
}

func TestTwitQuickAddFromListRefreshesList(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Other", to: "All", subject: "keep-first"}, testMsg{from: "Remote", to: "All", subject: "ignore-newest"})
	r := env.runCmd("LISTMSGS", env.caller, "", "\rIYQ")
	if len(env.mustDiskUser(2).TwitList) != 1 {
		t.Fatalf("no quick-add: %v\n%s", r.err, r.text())
	}
	after := strings.SplitN(r.text(), "Added to your Twit List.", 2)
	if len(after) != 2 || strings.Contains(after[1], "ignore-newest") || !strings.Contains(after[1], "keep-first") {
		t.Fatalf("rebuilt list did not apply quick-add: %v\n%s", r.err, r.text())
	}
}

func TestTwitListNamesRespectTerminalEncodingAndRemainLiteral(t *testing.T) {
	for _, mode := range []ansi.OutputMode{ansi.OutputModeUTF8, ansi.OutputModeCP437} {
		env := newMsgEnv(t)
		env.outputMode = mode
		env.caller.TwitList = []user.TwitEntry{{Name: "Jörg|12"}}
		r := env.runCmd("CFG_TWITLIST", env.caller, "", "Q\r")
		want := "Jörg|12"
		if mode == ansi.OutputModeCP437 {
			want = string([]byte{'J', 0x94, 'r', 'g', '|', '1', '2'})
		}
		if !strings.Contains(r.raw, want) {
			t.Fatalf("mode=%v: name was expanded or incorrectly encoded: %q", mode, r.raw)
		}
	}
}

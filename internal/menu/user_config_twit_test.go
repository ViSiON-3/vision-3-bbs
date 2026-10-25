package menu

import (
	"slices"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestCfgTwitListPersistenceAndRules checks editor persistence and rejected additions.
func TestCfgTwitListPersistenceAndRules(t *testing.T) {
	env := newMsgEnv(t)
	r := env.runCmd("CFG_TWITLIST", env.caller, "", "A\rCaller\rA\rSysop\rA\rRemote@21:1/100\rA\rRemote@21:1/100\rQ\r")
	if r.err != nil || !r.has("can't ignore yourself") || !r.has("can't be ignored") || !r.has("already on") {
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

// TestTwitSaveFailureRestoresSessionAndDisk checks rollback after a failed save.
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

// TestTwitReaderQuickAddAndNavigation checks immediate filtering and subsequent navigation.
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

// TestTwitListsPrivateMailAndThreads checks lists, private mail and thread filtering.
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

// TestTwitCounterRespectsOtherFiltersAndDeduplicates excludes inaccessible messages and repeated visits.
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

// TestTwitNewscanSummary checks one combined summary across scanned areas.
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

// TestTwitListCopiesAreIndependent checks manager read and write boundaries for slice aliases.
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

// TestTwitQuickAddSelfExemptAndAnonymous checks reader-side refusals.
func TestTwitQuickAddSelfExemptAndAnonymous(t *testing.T) {
	for _, tc := range []struct{ from, keys, want string }{
		{"Caller", "IQ", "can't ignore yourself"},
		{"Sysop", "IQ", "can't be ignored"},
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

// TestTwitQuickAddRealNamePreservesLocalExemption checks protected authors in real-name areas.
func TestTwitQuickAddRealNamePreservesLocalExemption(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: env.sysop.RealName, to: "All", subject: "real-name-post"})
	r := env.runCmd("READMSGS", env.caller, "", "IQ")
	if !r.has("can't be ignored") || len(env.mustDiskUser(2).TwitList) != 0 {
		t.Fatalf("%v\n%s", r.err, r.text())
	}
}

// TestTwitConfiguredCap checks the configured list limit through the editor.
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

// TestTwitKonfigLaunchAndReturn checks entry into the editor and form repaint.
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

// TestTwitLocalEntrySurvivesRenameAndPromotion checks stable IDs and refreshed exemptions.
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

// TestTwitQuickAddFromListRefreshesList checks that nested readers update their parent list.
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

// TestTwitListNamesRespectTerminalEncodingAndRemainLiteral checks UTF-8 and CP437 display without pipe expansion.
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

// TestTwitQuickAddLocalNetworkPost applies account rules to local echomail authors.
func TestTwitQuickAddLocalNetworkPost(t *testing.T) {
	for _, tc := range []struct {
		author  string
		wantID  int
		refusal string
	}{{"Caller", 0, "can't ignore yourself"}, {"Sysop", 0, "can't be ignored"}, {"Th3 D@rk", 3, ""}} {
		t.Run(tc.author, func(t *testing.T) {
			env := newMsgEnv(t)
			target, err := env.um.AddUser("password", "Th3 D@rk", "Dark Real Name", "Loc")
			if err != nil {
				t.Fatal(err)
			}
			area, err := env.e.MessageMgr.AddArea(message.MessageArea{Tag: "ECHO", Name: "Echo", AreaType: "echomail", EchoTag: "TEST", OriginAddr: "21:1/100", ACSRead: "*", ACSWrite: "*"})
			if err != nil {
				t.Fatal(err)
			}
			env.caller.CurrentMessageAreaID = area
			env.caller.CurrentMessageAreaTag = "ECHO"
			env.postMsgs(area, testMsg{from: tc.author, to: "All", subject: "local-network"})
			msg := env.mustMsg(area, 1)
			if msg.OrigAddr != "21:1/100" {
				t.Fatalf("not a local network post: %+v", msg)
			}
			result := env.runCmd("READMSGS", env.caller, "", "IYQ")
			list := env.mustDiskUser(2).TwitList
			if tc.refusal != "" {
				if !result.has(tc.refusal) || len(list) != 0 {
					t.Fatalf("account rule bypassed: %+v\n%s", list, result.text())
				}
				return
			}
			if len(list) != 1 || list[0].UserID != target.ID || list[0].Addr != "" {
				t.Fatalf("not local: %+v\n%s", list, result.text())
			}
		})
	}
}

// TestTwitQuickAddPromptEscapesAuthorCodes preserves the author's literal name.
func TestTwitQuickAddPromptEscapesAuthorCodes(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Remote|CL|12", to: "All", subject: "pipe-author"})
	result := env.runCmd("READMSGS", env.caller, "", "INQ")
	if !result.has("Add Remote|CL|12 to your Twit List?") {
		t.Fatalf("author interpreted as pipe codes:\n%s", result.text())
	}
}

// TestTwitQuickAddImportedAuthorKeepsOrigin separates remote names from local accounts.
func TestTwitQuickAddImportedAuthorKeepsOrigin(t *testing.T) {
	env := newMsgEnv(t)
	base, err := env.e.MessageMgr.GetBase(generalAreaID)
	if err != nil {
		t.Fatal(err)
	}
	msg := jam.NewMessage()
	msg.From = "Sysop"
	msg.To = "All"
	msg.Subject = "imported"
	msg.Text = "imported-body"
	msg.OrigAddr = "21:1/200"
	if _, err := base.WriteReceivedMessage(msg, jam.MsgTypeEchomailMsg); err != nil {
		t.Fatal(err)
	}
	if err := base.Close(); err != nil {
		t.Fatal(err)
	}
	result := env.runCmd("READMSGS", env.caller, "", "IYQ")
	list := env.mustDiskUser(2).TwitList
	if len(list) != 1 || list[0].UserID != 0 || list[0].Name != "Sysop" || list[0].Addr != "21:1/200" {
		t.Fatalf("import resolved as local: %+v\n%s", list, result.text())
	}
}

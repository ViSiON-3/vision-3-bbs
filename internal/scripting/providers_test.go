package scripting

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/google/uuid"
)

// newUserProviders seeds a UserMgr from a users.json in a temp dir and
// returns providers whose CurrentUser is the manager's copy of "Tester".
func newUserProviders(t *testing.T) (*Providers, string) {
	t.Helper()
	dir := t.TempDir()
	users := []*user.User{
		{ID: 1, Handle: "Sysop", RealName: "Sy Op", AccessLevel: 255, Validated: true, TimesCalled: 99,
			CreatedAt: time.Unix(1000, 0), LastLogin: time.Unix(2000, 0), PasswordHash: "secret-hash", PrivateNote: "private"},
		{ID: 7, Handle: "Tester", RealName: "Test User", AccessLevel: 50, TimesCalled: 12, GroupLocation: "Testville",
			MessagesPosted: 4, NumUploads: 2, NumDownloads: 3, FilePoints: 11, Validated: true},
	}
	raw, err := json.Marshal(users)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "users.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	um, err := user.NewUserManager(dir)
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	cur, ok := um.GetUserByID(7)
	if !ok {
		t.Fatal("seed user 7 missing")
	}
	return &Providers{UserMgr: um, CurrentUser: cur}, dir
}

// TestUserObject reads the current user's live fields through v3.user.
func TestUserObject(t *testing.T) {
	p, _ := newUserProviders(t)
	h := newHarness(t, harnessOpts{providers: p})
	got := h.eval(`[v3.user.id, v3.user.handle, v3.user.realName, v3.user.accessLevel, v3.user.timesCalled,
		v3.user.location, v3.user.messagesPosted, v3.user.uploads, v3.user.downloads, v3.user.filePoints,
		v3.user.validated].join("|")`).String()
	if want := "7|Tester|Test User|50|12|Testville|4|2|3|11|true"; got != want {
		t.Errorf("v3.user = %q, want %q", got, want)
	}
	// Accessors are live: a change on the Go side is visible immediately.
	p.CurrentUser.FilePoints = 42
	if got := h.eval(`v3.user.filePoints`).ToInteger(); got != 42 {
		t.Errorf("filePoints after Go update = %d, want 42", got)
	}
}

// TestUserSetAndSave covers the writable fields, real-name validation and
// persistence through UserMgr.
func TestUserSetAndSave(t *testing.T) {
	p, dir := newUserProviders(t)
	h := newHarness(t, harnessOpts{providers: p})
	h.mustRun(`
		v3.user.set("realName", "  New Name  ");
		v3.user.set("location", "Elsewhere");
		v3.user.set("screenWidth", 132);
		v3.user.set("screenHeight", 50);
		v3.user.set("accessLevel", 255);   // not writable: ignored
		v3.user.set("location");           // too few args: ignored
		v3.user.save();
	`)
	u := p.CurrentUser
	if u.RealName != "New Name" || u.GroupLocation != "Elsewhere" || u.ScreenWidth != 132 || u.ScreenHeight != 50 || u.AccessLevel != 50 {
		t.Errorf("user after set = %+v", u)
	}
	// Invalid real names are rejected and leave the field as it was.
	for _, bad := range []string{"", "   ", "Single", "ab"} {
		h.mustRun(`v3.user.set("realName", ` + jsQuote(bad) + `)`)
		if u.RealName != "New Name" {
			t.Fatalf("set(realName, %q) stored %q", bad, u.RealName)
		}
	}

	// save() wrote to disk: a fresh manager sees the change.
	um2, err := user.NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, ok := um2.GetUserByID(7)
	if !ok || reloaded.GroupLocation != "Elsewhere" || reloaded.RealName != "New Name" {
		t.Errorf("reloaded user = %+v, want saved changes", reloaded)
	}
}

// TestUserSaveErrorThrows: a save UserMgr rejects surfaces as a JS exception.
func TestUserSaveErrorThrows(t *testing.T) {
	p, _ := newUserProviders(t)
	p.CurrentUser = &user.User{ID: 999, Handle: "Ghost"}
	h := newHarness(t, harnessOpts{providers: p})
	if got := h.evalErr(`v3.user.save()`); !strings.Contains(got, "not found") {
		t.Errorf("save of unknown user threw %q, want not-found error", got)
	}
}

// TestUsersObject covers read-only user database lookups and checks
// sensitive fields are not exposed.
func TestUsersObject(t *testing.T) {
	p, _ := newUserProviders(t)
	h := newHarness(t, harnessOpts{providers: p})
	tests := []struct{ expr, want string }{
		{`v3.users.get("sysop").handle`, "Sysop"},
		{`String(v3.users.get("sysop").accessLevel)`, "255"},
		{`String(v3.users.get("sysop").createdAt)`, "1000"},
		{`String(v3.users.get("sysop").lastLogin)`, "2000"},
		{`String(v3.users.get("sysop").previousLogin)`, "0"},
		{`String(v3.users.get("sysop").passwordHash)`, "undefined"},
		{`String(v3.users.get("sysop").privateNote)`, "undefined"},
		{`String(v3.users.get("nobody"))`, "null"},
		{`String(v3.users.get())`, "null"},
		{`v3.users.getByID(7).realName`, "Test User"},
		{`String(v3.users.getByID(12345))`, "null"},
		{`String(v3.users.getByID())`, "null"},
		{`String(v3.users.count())`, "2"},
		{`v3.users.list().map(function(u){return u.id}).sort().join(",")`, "1,7"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
}

// newMessageProviders builds a MessageManager over two local JAM areas.
func newMessageProviders(t *testing.T) *Providers {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	areas := `[{"id":1,"tag":"GENERAL","name":"General","description":"Chat","base_path":"general","area_type":"local","conference_id":2},
	           {"id":2,"tag":"PRIVMAIL","name":"Private Mail","base_path":"privmail","area_type":"local"}]`
	if err := os.WriteFile(filepath.Join(cfg, "message_areas.json"), []byte(areas), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, err := message.NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	return &Providers{MessageMgr: mm}
}

// TestMessageObject posts and reads messages through v3.message and checks
// argument validation and error propagation.
func TestMessageObject(t *testing.T) {
	p := newMessageProviders(t)
	h := newHarness(t, harnessOpts{providers: p})

	if n := h.eval(`v3.message.post(1, {subject: "Hi", body: "Hello there"})`).ToInteger(); n != 1 {
		t.Fatalf("post returned %d, want 1", n)
	}
	if n := h.eval(`v3.message.post(1, {to: "Sysop", subject: "Re: Hi", body: "Reply"})`).ToInteger(); n != 2 {
		t.Fatalf("second post returned %d, want 2", n)
	}
	if n := h.eval(`v3.message.postPrivate(2, {to: "Sysop", subject: "Psst", body: "secret"})`).ToInteger(); n != 1 {
		t.Fatalf("postPrivate returned %d, want 1", n)
	}

	tests := []struct{ expr, want string }{
		{`v3.message.areas().map(function(a){return a.id+":"+a.tag+":"+a.type}).join(",")`, "1:GENERAL:local,2:PRIVMAIL:local"},
		{`var a = v3.message.area("GENERAL"); [a.name, a.description, a.conferenceID].join("|")`, "General|Chat|2"},
		{`String(v3.message.area("NOPE"))`, "null"},
		{`String(v3.message.area())`, "null"},
		{`String(v3.message.count(1))`, "2"},
		{`String(v3.message.count())`, "0"},
		{`String(v3.message.count(99))`, "0"},
		{`var m = v3.message.get(1, 1); [m.msgNum, m.from, m.to, m.subject, m.isPrivate, m.areaID].join("|")`, "1|Tester|All|Hi|false|1"},
		{`v3.message.get(1, 1).body.indexOf("Hello there") >= 0`, "true"},
		{`v3.message.get(2, 1).isPrivate`, "true"},
		{`v3.message.get(2, 1).to`, "Sysop"},
		{`String(v3.message.get(1))`, "null"},
		{`String(v3.message.get(1, 99))`, "null"},
		{`String(v3.message.get(99, 1))`, "null"},
		{`String(v3.message.newCount(1))`, "2"},
		{`String(v3.message.newCount())`, "0"},
		{`String(v3.message.totalCount())`, "3"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}

	errs := []struct{ expr, want string }{
		{`v3.message.post(1)`, "post requires arguments"},
		{`v3.message.postPrivate(2)`, "postPrivate requires arguments"},
		{`v3.message.postPrivate(2, {subject: "x"})`, "missing required 'to'"},
		{`v3.message.post(99, {subject: "x"})`, "not found"},
		{`v3.message.postPrivate(99, {to: "x"})`, "not found"},
	}
	for _, tt := range errs {
		if got := h.evalErr(tt.expr); !strings.Contains(got, tt.want) {
			t.Errorf("%s threw %q, want %q", tt.expr, got, tt.want)
		}
	}
}

// TestFileObject lists and searches file areas through v3.file.
func TestFileObject(t *testing.T) {
	dataDir, cfgDir := t.TempDir(), t.TempDir()
	areas := `[{"id": 1, "tag": "UTILS", "name": "Utilities", "description": "Tools", "path": "utils", "conference_id": 3},
	           {"id": 2, "tag": "GAMES", "name": "Games", "path": "games"}]`
	if err := os.WriteFile(filepath.Join(cfgDir, "file_areas.json"), []byte(areas), 0o644); err != nil {
		t.Fatal(err)
	}
	fm, err := file.NewFileManager(dataDir, cfgDir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}
	up := time.Unix(1700000000, 0)
	for _, r := range []file.FileRecord{
		{ID: uuid.New(), AreaID: 1, Filename: "PKZIP.ZIP", Description: "Archiver", Size: 1234, UploadedAt: up, UploadedBy: "Sysop", DownloadCount: 5},
		{ID: uuid.New(), AreaID: 1, Filename: "LIST.ZIP", Description: "File lister", Size: 10, UploadedAt: up},
		{ID: uuid.New(), AreaID: 2, Filename: "DOOM.ZIP", Description: "Shareware game", Size: 99, UploadedAt: up},
	} {
		if err := fm.AddFileRecord(r); err != nil {
			t.Fatalf("AddFileRecord: %v", err)
		}
	}
	h := newHarness(t, harnessOpts{providers: &Providers{FileMgr: fm}})

	tests := []struct{ expr, want string }{
		{`v3.file.areas().map(function(a){return a.id+":"+a.tag}).sort().join(",")`, "1:UTILS,2:GAMES"},
		{`var a = v3.file.area("UTILS"); [a.id, a.name, a.description, a.conferenceID].join("|")`, "1|Utilities|Tools|3"},
		{`String(v3.file.area("NOPE"))`, "null"},
		{`String(v3.file.area())`, "null"},
		{`v3.file.list(1).map(function(f){return f.filename}).sort().join(",")`, "LIST.ZIP,PKZIP.ZIP"},
		{`String(v3.file.list().length)`, "0"},
		{`var f = v3.file.list(1).filter(function(f){return f.filename=="PKZIP.ZIP"})[0];
		  [f.areaID, f.description, f.size, f.uploadedAt, f.uploadedBy, f.downloadCount, f.id.length].join("|")`, "1|Archiver|1234|1700000000|Sysop|5|36"},
		{`String(v3.file.count(1))`, "2"},
		{`String(v3.file.count())`, "0"},
		{`String(v3.file.count(99))`, "0"},
		{`v3.file.search("game").map(function(f){return f.filename}).join(",")`, "DOOM.ZIP"},
		{`String(v3.file.search().length)`, "0"},
		{`String(v3.file.totalCount())`, "3"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
}

// newNodeRegistry registers nodes 1 (self), 2 (visible) and 4 (invisible).
func newNodeRegistry() (*session.SessionRegistry, map[int]*session.BbsSession) {
	reg := session.NewSessionRegistry()
	nodes := map[int]*session.BbsSession{
		3: {NodeID: 3, User: &user.User{Handle: "Tester"}, Activity: "Running script", LastActivity: time.Now()},
		1: {NodeID: 1, User: &user.User{Handle: "Alice"}, Activity: "Reading mail", LastActivity: time.Now().Add(-90 * time.Second)},
		4: {NodeID: 4, User: &user.User{Handle: "Ghost"}, Activity: "Lurking", LastActivity: time.Now(), Invisible: true},
		5: {NodeID: 5, Activity: "Logging in", LastActivity: time.Now()},
	}
	for _, s := range nodes {
		reg.Register(s)
	}
	return reg, nodes
}

// TestNodesObject covers who's-online listing (with invisibility honoured
// for non-sysops) and inter-node paging.
func TestNodesObject(t *testing.T) {
	reg, nodes := newNodeRegistry()
	h := newHarness(t, harnessOpts{providers: &Providers{SessionRegistry: reg}})

	tests := []struct{ expr, want string }{
		{`v3.nodes.list().map(function(n){return n.node+":"+n.handle+":"+n.activity+":"+n.invisible}).join(",")`,
			"1:Alice:Reading mail:false,3:Tester:Running script:false,5::Logging in:false"},
		{`v3.nodes.list()[0].idle >= 89`, "true"},
		{`String(v3.nodes.count())`, "3"},
		{`String(v3.nodes.send(1, "hello"))`, "true"},
		{`String(v3.nodes.send(42, "hello"))`, "false"},
		{`String(v3.nodes.broadcast())`, "undefined"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
	if got := nodes[1].DrainPages(); len(got) != 1 || got[0] != "[Node 3 - Tester] hello" {
		t.Errorf("node 1 pages = %q", got)
	}
	if got := h.evalErr(`v3.nodes.send(1)`); !strings.Contains(got, "send requires arguments") {
		t.Errorf("send(1) threw %q", got)
	}

	h.mustRun(`v3.nodes.broadcast("all hands")`)
	for id, s := range nodes {
		pages := s.DrainPages()
		if id == 3 {
			if len(pages) != 0 {
				t.Errorf("broadcast paged self: %q", pages)
			}
			continue
		}
		if len(pages) != 1 || pages[0] != "[Node 3 - Tester] all hands" {
			t.Errorf("node %d pages = %q", id, pages)
		}
	}
}

// TestNodesSysopSeesInvisible: access level >= 200 includes invisible nodes.
func TestNodesSysopSeesInvisible(t *testing.T) {
	reg, _ := newNodeRegistry()
	h := newHarness(t, harnessOpts{
		providers: &Providers{SessionRegistry: reg},
		session:   func(sc *SessionContext) { sc.AccessLevel = 255 },
	})
	if got := h.eval(`String(v3.nodes.count())`).String(); got != "4" {
		t.Errorf("sysop count = %s, want 4", got)
	}
	if got := h.eval(`v3.nodes.list().filter(function(n){return n.invisible}).map(function(n){return n.handle}).join()`).String(); got != "Ghost" {
		t.Errorf("sysop invisible list = %q, want Ghost", got)
	}
}

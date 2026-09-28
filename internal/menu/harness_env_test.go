package menu

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// menuEnv is a complete, isolated BBS for handler tests: an executor wired
// the way NewExecutor wires it, running on the shipped defaults.
//
//   - configs/ is a temp copy of templates/configs, so handlers that load or
//     save config files see the factory values and cannot touch the repo.
//   - data/ is an empty temp directory (ServerConfig.DataDir).
//   - The menu set is the repo's menus/v3, used read-only for its ANSI
//     screens, menus and theme. Tests that write to the menu set must build
//     their own copy.
//   - The message, file and conference managers are real, over the shipped
//     area definitions. The user manager is too, over a users.json seeded
//     with a sysop (ID 1, level 255) and a validated caller (ID 2, level 10),
//     so handlers that save users work and tests can reload to check.
//
// Tweak it with the Set* methods on env.e before calling run.
type menuEnv struct {
	t      *testing.T
	e      *MenuExecutor
	um     *user.UserMgr
	sysop  *user.User
	caller *user.User
	root   string // temp root holding configs/ and data/

	// remoteIP is the address sessions connect from; empty means 127.0.0.1.
	remoteIP string
	// outputMode is the session's negotiated encoding. It sets both the
	// handler's cmdCtx.outputMode and the session-level mode the line reader
	// uses to decode typed extended characters. Defaults to UTF-8.
	outputMode ansi.OutputMode
}

// defaultTestUsers are the users every menuEnv starts with.
func defaultTestUsers() []*user.User {
	return []*user.User{
		{ID: 1, Handle: "Sysop", RealName: "Sam Sysop", AccessLevel: 255, Validated: true, TimeLimit: 60},
		{ID: 2, Handle: "Caller", RealName: "Carl Caller", AccessLevel: 10, Validated: true, TimeLimit: 60},
	}
}

// newMenuEnv builds a menuEnv. Every piece lives under t.TempDir() except
// the read-only menu set.
func newMenuEnv(t *testing.T) *menuEnv {
	t.Helper()
	root := t.TempDir()
	cfgDir := filepath.Join(root, "configs")
	dataDir := filepath.Join(root, "data")
	for _, d := range []string{cfgDir, dataDir, filepath.Join(dataDir, "logs")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.WalkDir(configtemplates.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := configtemplates.FS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(cfgDir, p), b, 0o644)
	}); err != nil {
		t.Fatalf("copy config templates: %v", err)
	}

	menuSet, err := filepath.Abs(filepath.Join("..", "..", "menus", "v3"))
	if err != nil {
		t.Fatal(err)
	}
	strs, err := config.LoadStrings(cfgDir)
	if err != nil {
		t.Fatalf("LoadStrings: %v", err)
	}
	theme, err := config.LoadThemeConfig(menuSet)
	if err != nil {
		t.Fatalf("LoadThemeConfig: %v", err)
	}
	srv, err := config.LoadServerConfig(cfgDir)
	if err != nil {
		t.Fatalf("LoadServerConfig: %v", err)
	}
	srv.DataDir = dataDir

	mm, err := message.NewMessageManager(dataDir, cfgDir, srv.BoardName, nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	fm, err := file.NewFileManager(dataDir, cfgDir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}
	cm, err := conference.NewConferenceManager(cfgDir)
	if err != nil {
		t.Fatalf("NewConferenceManager: %v", err)
	}

	e := NewExecutor(menuSet, cfgDir, filepath.Join(root, "assets"), nil, nil,
		strs, theme, srv, mm, fm, cm, nil, nil, session.NewSessionRegistry(), nil)

	env := &menuEnv{t: t, e: e, root: root, outputMode: ansi.OutputModeUTF8}
	env.writeUsers(defaultTestUsers()...)
	return env
}

// writeUsers replaces users.json with exactly users and reloads env.um,
// env.sysop (ID 1) and env.caller (ID 2) from it; either is nil if absent.
// Writing the file directly skips AddUser's bcrypt cost and can set fields
// AddUser can't (Validated, AccessLevel, DeletedUser and so on).
func (env *menuEnv) writeUsers(users ...*user.User) {
	env.t.Helper()
	b, err := json.Marshal(users)
	if err != nil {
		env.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.dataDir(), "users.json"), b, 0o644); err != nil {
		env.t.Fatal(err)
	}
	um, err := user.NewUserManager(env.dataDir())
	if err != nil {
		env.t.Fatalf("NewUserManager: %v", err)
	}
	env.um = um
	env.sysop, _ = um.GetUserByID(1)
	env.caller, _ = um.GetUserByID(2)
}

// seedUsers is writeUsers with the default sysop and caller plus extra.
func (env *menuEnv) seedUsers(extra ...*user.User) {
	env.t.Helper()
	env.writeUsers(append(defaultTestUsers(), extra...)...)
}

// diskUser reloads users.json through a fresh manager and returns user id as
// saved, so assertions see what a handler persisted rather than in-memory
// state.
func (env *menuEnv) diskUser(id int) (*user.User, bool) {
	env.t.Helper()
	um, err := user.NewUserManager(env.dataDir())
	if err != nil {
		env.t.Fatalf("reload users: %v", err)
	}
	return um.GetUserByID(id)
}

// mustDiskUser is diskUser for a user that must still exist.
func (env *menuEnv) mustDiskUser(id int) *user.User {
	env.t.Helper()
	u, ok := env.diskUser(id)
	if !ok {
		env.t.Fatalf("user %d missing after reload", id)
	}
	return u
}

// sub returns a copy of env bound to subtest t, sharing the same BBS, so
// harness failures and cleanups land on the subtest. Use it when subtests
// share one env: env.sub(t).runCmd(...).
func (env *menuEnv) sub(t *testing.T) *menuEnv {
	cp := *env
	cp.t = t
	return &cp
}

// cfgDir is the temp configs/ directory (RootConfigPath).
func (env *menuEnv) cfgDir() string { return env.e.RootConfigPath }

// dataDir is the temp data/ directory (ServerConfig.DataDir).
func (env *menuEnv) dataDir() string { return env.e.GetServerConfig().DataDir }

// runResult is what a handler did with one scripted session.
type runResult struct {
	raw  string     // everything written to the session, escapes included
	user *user.User // the handler's authenticatedUser result
	next string     // the handler's nextAction result
	err  error      // the handler's error, with end-of-input already cleared
}

// text is the output with ANSI escapes stripped and CP437/UTF-8 left as-is,
// for plain-text assertions.
func (r runResult) text() string { return testAnsiEscape.ReplaceAllString(r.raw, "") }

// has reports whether the stripped output contains every one of want.
func (r runResult) has(want ...string) bool {
	txt := r.text()
	for _, w := range want {
		if !strings.Contains(txt, w) {
			return false
		}
	}
	return true
}

// run calls fn as u (nil for a caller who has not logged in) with args,
// feeding it input as keystrokes. Once input runs out every read reports a
// disconnect, so the handler unwinds instead of blocking; the resulting
// io.EOF or errInputAborted is treated as normal and cleared from err.
//
// Each call gets a fresh session, so per-session state (the input handler,
// output mode, idle timeout) never leaks between calls. The session connects
// from env.remoteIP in env.outputMode. It fails the test if the handler has
// not returned after 10 seconds.
func (env *menuEnv) run(fn RunnableFunc, u *user.User, args, input string) runResult {
	env.t.Helper()
	ts := newTestSession(input)
	if env.remoteIP != "" {
		ts.addr = &net.TCPAddr{IP: net.ParseIP(env.remoteIP), Port: 2222}
	}
	SetSessionOutputMode(ts, env.outputMode)
	env.t.Cleanup(func() {
		resetSessionIH(ts)
		ClearSessionOutputMode(ts)
	})
	c := &cmdCtx{
		e:                env.e,
		s:                ts,
		terminal:         newTestTerminal(ts),
		userManager:      env.um,
		currentUser:      u,
		nodeNumber:       1,
		sessionStartTime: time.Now(),
		outputMode:       env.outputMode,
		termWidth:        80,
		termHeight:       24,
	}

	type ret struct {
		u    *user.User
		next string
		err  error
	}
	done := make(chan ret, 1)
	go func() {
		u, next, err := fn(c, args)
		done <- ret{u, next, err}
	}()
	select {
	case r := <-done:
		if errors.Is(r.err, io.EOF) || errors.Is(r.err, errInputAborted) {
			r.err = nil
		}
		return runResult{raw: ts.output(), user: r.u, next: r.next, err: r.err}
	case <-time.After(10 * time.Second):
		// Don't read ts.out here: the handler goroutine may still be writing.
		env.t.Fatalf("handler still running 10s after its input ran out")
		return runResult{}
	}
}

// runFrom is run with no args for a session connecting from ip.
func (env *menuEnv) runFrom(ip string, fn RunnableFunc, u *user.User, input string) runResult {
	env.t.Helper()
	prev := env.remoteIP
	env.remoteIP = ip
	defer func() { env.remoteIP = prev }()
	return env.run(fn, u, "", input)
}

// runCmd runs the RUN: target named cmd from the executor's registry, the
// same lookup a menu's command line uses.
func (env *menuEnv) runCmd(cmd string, u *user.User, args, input string) runResult {
	env.t.Helper()
	fn, ok := env.e.RunRegistry[cmd]
	if !ok {
		env.t.Fatalf("no runnable registered as %q", cmd)
	}
	return env.run(fn, u, args, input)
}

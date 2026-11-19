package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// testLoginFixture is a user manager with one test account (ID 20) and one
// ordinary caller (ID 2), and a logger that records into buf.
func testLoginFixture(t *testing.T) (*user.UserMgr, *user.User, *user.User, *bytes.Buffer, *slog.Logger) {
	t.Helper()
	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	tester := &user.User{ID: 20, Handle: "Robot", AccessLevel: 10, Validated: true, TestAccount: true}
	caller := &user.User{ID: 2, Handle: "Caller", AccessLevel: 10, Validated: true}
	var buf bytes.Buffer
	return um, tester, caller, &buf, slog.New(slog.NewJSONHandler(&buf, nil))
}

// login sets u as the caller on a new session on node, as main.go does on
// each authentication path, and returns the session.
func login(t *testing.T, u *user.User, node int, log *slog.Logger) *session.BbsSession {
	t.Helper()
	sess := &session.BbsSession{NodeID: node, StartTime: time.Now()}
	if got := setSessionUser(sess, u, node, log); got != u.TestAccount {
		t.Fatalf("setSessionUser(%s) = %v, want %v", u.Handle, got, u.TestAccount)
	}
	if sess.User != u {
		t.Fatalf("setSessionUser(%s) did not set the caller", u.Handle)
	}
	return sess
}

// hangUp records the call as the disconnect path in main.go does.
func hangUp(um *user.UserMgr, sess *session.BbsSession, log *slog.Logger) {
	sess.Mutex.RLock()
	u, node, start := sess.User, sess.NodeID, sess.StartTime
	sess.Mutex.RUnlock()
	recordCall(um, sess, user.CallRecord{UserID: u.ID, Handle: u.Handle, NodeID: node, ConnectTime: start, DisconnectTime: time.Now()}, log)
}

// A test account's call writes no caller-history record and does not move
// the board's total call count; an ordinary caller's call does both.
func TestTestLoginLeavesNoCallRecord(t *testing.T) {
	um, tester, caller, _, log := testLoginFixture(t)

	hangUp(um, login(t, tester, 1, log), log)
	if got := um.GetLastCallers(); len(got) != 0 {
		t.Fatalf("test login wrote caller history: %+v", got)
	}
	if got := um.GetTotalCalls(); got != 0 {
		t.Fatalf("test login raised total calls to %d", got)
	}

	hangUp(um, login(t, caller, 2, log), log)
	got := um.GetLastCallers()
	if len(got) != 1 || got[0].UserID != caller.ID {
		t.Fatalf("ordinary call history = %+v, want one record for the caller", got)
	}
	if n := um.GetTotalCalls(); n != 1 {
		t.Fatalf("total calls after one ordinary call = %d, want 1", n)
	}
}

// The day's call count leaves out a test account that is online; an
// ordinary caller online is counted.
func TestTestLoginNotCountedToday(t *testing.T) {
	_, tester, caller, _, log := testLoginFixture(t)
	now := time.Now()
	active := []*session.BbsSession{login(t, tester, 1, log)}
	if got := countCallsToday(nil, active, now); got != 0 {
		t.Fatalf("calls today with only a test account online = %d, want 0", got)
	}
	active = append(active, login(t, caller, 2, log))
	if got := countCallsToday(nil, active, now); got != 1 {
		t.Fatalf("calls today with a test account and a caller online = %d, want 1", got)
	}
}

// A test account's session is hidden from the network's Who's Online and is
// never announced (the session is invisible); an ordinary caller's is not.
func TestTestLoginHiddenFromNetwork(t *testing.T) {
	_, tester, caller, _, log := testLoginFixture(t)
	users := &fakeUsers{byID: map[int]*user.User{
		20: {ID: 20, Handle: "Robot", CentralPresence: true, TestAccount: true},
		2:  {ID: 2, Handle: "Caller", CentralPresence: true},
	}}
	ts, cs := login(t, tester, 1, log), login(t, caller, 2, log)
	ts.SetCentralReady(true)
	cs.SetCentralReady(true)
	if !ts.Invisible || !ts.TestLogin {
		t.Fatalf("test session: invisible=%v testLogin=%v, want both", ts.Invisible, ts.TestLogin)
	}
	if cs.Invisible || cs.TestLogin {
		t.Fatalf("ordinary session: invisible=%v testLogin=%v, want neither", cs.Invisible, cs.TestLogin)
	}
	// Marked a test login without the invisible flag: still left out.
	only := &session.BbsSession{NodeID: 3, User: tester, StartTime: time.Now(), TestLogin: true}
	only.SetCentralReady(true)
	got := newCentralActivitySource(fakeSessions{ts, cs, only}, users).CentralPresence(false)
	if len(got) != 1 || got[0].Handle != "Caller" {
		t.Fatalf("network presence = %+v, want only Caller", got)
	}
}

// The server log still records a test login, with its node and account,
// marked as a test login; an ordinary login gets no such line.
func TestTestLoginIsLogged(t *testing.T) {
	um, tester, caller, buf, log := testLoginFixture(t)
	hangUp(um, login(t, tester, 7, log), log)

	var marked []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if rec["testLogin"] == true {
			marked = append(marked, rec)
		}
	}
	if len(marked) == 0 {
		t.Fatalf("no log line marked testLogin:\n%s", buf)
	}
	for _, rec := range marked {
		if rec["node"] != float64(7) || rec["user"] != "Robot" || rec["userID"] != float64(20) {
			t.Errorf("log line lacks node/account: %v", rec)
		}
	}

	buf.Reset()
	hangUp(um, login(t, caller, 2, log), log)
	if strings.Contains(buf.String(), "testLogin") {
		t.Errorf("ordinary login logged as a test login:\n%s", buf)
	}
}

// The invisible-logon prompt is never shown to a test account, whatever its
// level; an ordinary account at the invisible level still gets it.
func TestTestAccountGetsNoInvisiblePrompt(t *testing.T) {
	cfg := config.ServerConfig{CoSysOpLevel: 250, SysOpLevel: 255, InvisibleLevel: 250}
	if offersInvisibleLogon(&user.User{AccessLevel: 255, TestAccount: true}, cfg) {
		t.Error("test account at sysop level was offered the invisible-logon prompt")
	}
	if offersInvisibleLogon(&user.User{AccessLevel: 10, TestAccount: true}, cfg) {
		t.Error("low-level test account was offered the invisible-logon prompt")
	}
	if !offersInvisibleLogon(&user.User{AccessLevel: 255}, cfg) {
		t.Error("ordinary sysop was not offered the invisible-logon prompt")
	}
	if offersInvisibleLogon(&user.User{AccessLevel: 10}, cfg) {
		t.Error("ordinary caller was offered the invisible-logon prompt")
	}
	cfg.InvisibleLevel = 0 // falls back to the co-sysop level
	if !offersInvisibleLogon(&user.User{AccessLevel: 250}, cfg) {
		t.Error("co-sysop not offered the prompt when invisibleLevel is 0")
	}
}

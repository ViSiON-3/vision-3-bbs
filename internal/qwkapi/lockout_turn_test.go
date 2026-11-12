package qwkapi

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// turnServer is a server whose guard and account store a test can reach.
func turnServer(t *testing.T) (http.Handler, *accountStore, *fakeGuard) {
	t.Helper()
	accounts := newAccountStore(testAccount())
	g := newFakeGuard()
	deps := testDeps(t, accounts, &fakeSvc{})
	deps.Guard = g
	return newTestServer(t, deps), accounts, g
}

// together runs n calls of fn at the same moment and waits for them all.
func together(t *testing.T, n int, fn func(i int)) {
	t.Helper()
	var start, wg sync.WaitGroup
	start.Add(1)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start.Wait()
			fn(i)
		}()
	}
	start.Done()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("logins still running after 10s")
	}
}

// slowCheck makes each password or key check take a while, as bcrypt does,
// so logins sent together are all in flight at once.
func slowCheck(accounts *accountStore) {
	accounts.checking = func() { time.Sleep(20 * time.Millisecond) }
}

// A lockout another login starts while this password is being checked
// stays: the right password doesn't clear it or get a token.
func TestAPI_LockoutDuringPasswordCheckKeepsIt(t *testing.T) {
	h, accounts, g := turnServer(t)
	const ip = "203.0.113.20"
	accounts.checking = func() { g.lockIP(ip) }

	rr := loginFrom(h, ip, "felonius", "pw")
	if rr.Code != http.StatusTooManyRequests || bodyError(t, rr.Body.Bytes()) != "locked_out" {
		t.Fatalf("status = %d %s, want 429 locked_out", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After on the refusal")
	}
	if g.cleared[ip] != 0 {
		t.Errorf("the lockout was cleared %d times", g.cleared[ip])
	}
}

// Wrong passwords sent together from one IP are checked one at a time, so no
// more are tried than the lockout allows: the rest find the IP locked out.
func TestAPI_ConcurrentPasswordGuessesFromOneIP(t *testing.T) {
	h, accounts, g := turnServer(t)
	g.max = 3
	slowCheck(accounts)
	const ip = "198.51.100.30"

	codes := make([]int, 5) // the login rate limit's worth
	together(t, len(codes), func(i int) {
		codes[i] = loginFrom(h, ip, "felonius", "guess").Code
	})

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failed[ip] != g.max {
		t.Errorf("%d wrong passwords tried, want %d (the lockout's limit); statuses %v", g.failed[ip], g.max, codes)
	}
	locked := 0
	for _, c := range codes {
		if c == http.StatusTooManyRequests {
			locked++
		}
	}
	if locked != len(codes)-g.max {
		t.Errorf("%d logins refused as locked out, want %d; statuses %v", locked, len(codes)-g.max, codes)
	}
}

// deviceLoginWith posts a device login for d with the given challenge and
// signature.
func deviceLoginWith(h http.Handler, d *testDevice, ch, sig string) *httptest.ResponseRecorder {
	return postJSON(h, "/api/qwk/login/device", "", deviceLoginRequest{DeviceID: d.id, Challenge: ch, Signature: sig})
}

// The same for a device login: a lockout started while the key is checked
// stays, and no session is begun, so the call isn't recorded.
func TestDevice_LockoutDuringKeyCheckKeepsIt(t *testing.T) {
	h, accounts, g := turnServer(t)
	d := newTestDevice(t)
	pair(t, h, passwordLogin(t, h), d)
	ch := challenge(t, h, d)
	clearedBefore := g.cleared["192.0.2.1"]
	accounts.checking = func() { g.lockIP("192.0.2.1") } // httptest's address

	rr := deviceLoginWith(h, d, ch, d.sign(ch))
	if rr.Code != http.StatusTooManyRequests || bodyError(t, rr.Body.Bytes()) != "locked_out" {
		t.Fatalf("status = %d %s, want 429 locked_out", rr.Code, rr.Body.String())
	}
	if g.cleared["192.0.2.1"] != clearedBefore {
		t.Error("the lockout was cleared")
	}
	if accounts.sessions != 0 {
		t.Errorf("%d sessions begun for a refused login", accounts.sessions)
	}
}

// Bad signatures sent together from one IP are checked one at a time too.
func TestDevice_ConcurrentBadSignaturesFromOneIP(t *testing.T) {
	h, accounts, g := turnServer(t)
	d := newTestDevice(t)
	pair(t, h, passwordLogin(t, h), d)
	g.max = 3
	const ip = "192.0.2.1" // httptest's address

	// The password login above used one of the five logins a minute allows.
	chs := make([]string, 4)
	for i := range chs {
		chs[i] = challenge(t, h, d)
	}
	slowCheck(accounts)
	bad := base64.StdEncoding.EncodeToString(make([]byte, 64))
	codes := make([]int, len(chs))
	together(t, len(chs), func(i int) {
		codes[i] = deviceLoginWith(h, d, chs[i], bad).Code
	})

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failed[ip] != g.max {
		t.Errorf("%d bad signatures tried, want %d (the lockout's limit); statuses %v", g.failed[ip], g.max, codes)
	}
}

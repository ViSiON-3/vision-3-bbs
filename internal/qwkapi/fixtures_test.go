package qwkapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

const testBoardID = "TESTBBS"

// admitAll is a DenyLogin hook that refuses no one.
func admitAll(*user.User) string { return "" }

// testAccount is the account most tests log in as: felonius, password "pw".
func testAccount() *user.User {
	return &user.User{ID: 7, Handle: "felonius", AccessLevel: 20, PasswordHash: "hash-1"}
}

// accountStore is an Authenticator and DeviceStore over live accounts the
// test can change between requests, as a sysop or the terminal would. Every
// account's password is "pw". Its device rules mirror UserMgr's: devices are
// stamped with the password they were paired under.
type accountStore struct {
	mu    sync.Mutex
	users []*user.User
	syncs int
}

func newAccountStore(users ...*user.User) *accountStore { return &accountStore{users: users} }

func (a *accountStore) Authenticate(handle, password string) (*user.User, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, u := range a.users {
		if strings.EqualFold(u.Handle, handle) && !u.DeletedUser && password == "pw" {
			c := *u
			return &c, true
		}
	}
	return nil, false
}

func (a *accountStore) SyncFromDisk() {
	a.mu.Lock()
	a.syncs++
	a.mu.Unlock()
}

func (a *accountStore) GetUserByID(id int) (*user.User, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if u := a.byID(id); u != nil {
		c := *u
		return &c, true
	}
	return nil, false
}

func (a *accountStore) BeginSession(handle string) (*user.User, bool) {
	return a.Authenticate(handle, "pw")
}

func (a *accountStore) AddAPIDevice(userID int, d user.APIDevice, max int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	u := a.byID(userID)
	if u == nil {
		return user.ErrUserNotFound
	}
	valid := u.ValidAPIDevices()
	for _, e := range valid {
		if e.PublicKey == d.PublicKey {
			return user.ErrDeviceKeyExists
		}
	}
	if len(valid) >= max {
		return user.ErrTooManyDevices
	}
	d.PasswordStamp = user.PasswordStamp(u.PasswordHash)
	u.APIDevices = append(valid, d)
	return nil
}

func (a *accountStore) RemoveAPIDevice(userID int, deviceID string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u := a.byID(userID)
	if u == nil {
		return false, user.ErrUserNotFound
	}
	var out []user.APIDevice
	removed := false
	for _, d := range u.ValidAPIDevices() {
		if d.ID == deviceID {
			removed = true
			continue
		}
		out = append(out, d)
	}
	u.APIDevices = out
	return removed, nil
}

func (a *accountStore) TouchAPIDevice(userID int, deviceID string, at time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if u := a.byID(userID); u != nil {
		for i := range u.APIDevices {
			if u.APIDevices[i].ID == deviceID {
				u.APIDevices[i].LastUsed = at
			}
		}
	}
	return nil
}

func (a *accountStore) FindAPIDevice(deviceID string) (*user.User, user.APIDevice, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, u := range a.users {
		if d, ok := u.APIDevice(deviceID); ok {
			c := *u
			return &c, d, true
		}
	}
	return nil, user.APIDevice{}, false
}

// change applies fn to the stored account under the store's lock.
func (a *accountStore) change(id int, fn func(*user.User)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if u := a.byID(id); u != nil {
		fn(u)
	}
}

func (a *accountStore) syncCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.syncs
}

func (a *accountStore) byID(id int) *user.User {
	for _, u := range a.users {
		if u.ID == id {
			return u
		}
	}
	return nil
}

// testDeps is a working dependency set over accounts and svc.
func testDeps(t *testing.T, accounts *accountStore, svc PacketService) Deps {
	t.Helper()
	return Deps{
		Config:    config.QWKAPIConfig{},
		ConfigDir: t.TempDir(),
		Users:     accounts,
		Devices:   accounts,
		BoardID:   testBoardID,
		Service:   svc,
		AuthorizeFor: func(u *user.User) func(*message.MessageArea) bool {
			return func(*message.MessageArea) bool { return true }
		},
		TwitMatcherFor: func(*user.User) *user.TwitMatcher { return nil },
		DenyLogin:      admitAll,
	}
}

func newTestServer(t *testing.T, deps Deps) http.Handler {
	t.Helper()
	s, err := NewServer(deps)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s.Handler()
}

// testDevice is a phone's key pair as a client would hold it.
type testDevice struct {
	id   string
	priv ed25519.PrivateKey
	pub  string // base64 PKIX
}

func newTestDevice(t *testing.T) *testDevice {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return &testDevice{priv: priv, pub: base64.StdEncoding.EncodeToString(der)}
}

func (d *testDevice) sign(challenge string) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(d.priv, deviceLoginMessage(testBoardID, d.id, challenge)))
}

func postJSON(h http.Handler, path, tok string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := clientReq("POST", path, b)
	if tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

// passwordLogin logs felonius in with the password, returning a pair token.
func passwordLogin(t *testing.T, h http.Handler) string {
	t.Helper()
	rr := postJSON(h, "/api/qwk/login", "", loginRequest{Handle: "felonius", Password: "pw"})
	if rr.Code != http.StatusOK {
		t.Fatalf("login status = %d: %s", rr.Code, rr.Body.String())
	}
	var resp loginResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Token == "" || resp.Scope != string(scopePair) {
		t.Fatalf("login response = %+v, want a pair token", resp)
	}
	return resp.Token
}

// pair registers d using pairTok.
func pair(t *testing.T, h http.Handler, pairTok string, d *testDevice) {
	t.Helper()
	rr := postJSON(h, "/api/qwk/devices", pairTok, pairRequest{Name: "Test phone", PublicKey: d.pub})
	if rr.Code != http.StatusCreated {
		t.Fatalf("pair status = %d: %s", rr.Code, rr.Body.String())
	}
	var resp pairResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.DeviceID == "" || resp.BoardID != testBoardID {
		t.Fatalf("pair response = %+v", resp)
	}
	d.id = resp.DeviceID
}

// challenge requests a challenge for d.
func challenge(t *testing.T, h http.Handler, d *testDevice) string {
	t.Helper()
	rr := postJSON(h, "/api/qwk/challenge", "", challengeRequest{DeviceID: d.id})
	if rr.Code != http.StatusOK {
		t.Fatalf("challenge status = %d: %s", rr.Code, rr.Body.String())
	}
	var resp challengeResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	return resp.Challenge
}

// deviceLogin signs a fresh challenge with d and returns the response.
func deviceLogin(t *testing.T, h http.Handler, d *testDevice) *httptest.ResponseRecorder {
	t.Helper()
	ch := challenge(t, h, d)
	return postJSON(h, "/api/qwk/login/device", "", deviceLoginRequest{DeviceID: d.id, Challenge: ch, Signature: d.sign(ch)})
}

// deviceToken logs d in, failing the test unless a device token comes back.
func deviceToken(t *testing.T, h http.Handler, d *testDevice) string {
	t.Helper()
	rr := deviceLogin(t, h, d)
	if rr.Code != http.StatusOK {
		t.Fatalf("device login status = %d: %s", rr.Code, rr.Body.String())
	}
	var resp loginResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Token == "" || resp.Scope != string(scopeDevice) {
		t.Fatalf("device login response = %+v, want a device token", resp)
	}
	return resp.Token
}

// login runs the whole client flow — password login, pair a new key, device
// login — and returns a device token, the kind packets need.
func login(t *testing.T, h http.Handler) string {
	t.Helper()
	d := newTestDevice(t)
	pair(t, h, passwordLogin(t, h), d)
	return deviceToken(t, h, d)
}

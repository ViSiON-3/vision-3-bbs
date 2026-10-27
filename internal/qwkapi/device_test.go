package qwkapi

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func deviceServer(t *testing.T) (http.Handler, *accountStore, *fakeGuard) {
	t.Helper()
	accounts := newAccountStore(testAccount())
	g := newFakeGuard()
	deps := testDeps(t, accounts, &fakeSvc{})
	deps.Guard = g
	deps.DenyLogin = func(u *user.User) string {
		if u.AccessLevel < 10 {
			return DenyLogonLevel
		}
		return ""
	}
	return newTestServer(t, deps), accounts, g
}

func bodyError(t *testing.T, body []byte) string {
	t.Helper()
	var m map[string]string
	_ = json.Unmarshal(body, &m)
	return m["error"]
}

// "Pairing only": a password login can register a device but never touch
// packets, and a device token cannot register further devices.
func TestDevice_ScopesAreEnforced(t *testing.T) {
	h, _, _ := deviceServer(t)
	pairTok := passwordLogin(t, h)
	for _, path := range []string{"/api/qwk/packet", "/api/qwk/reply"} {
		method := "GET"
		if path == "/api/qwk/reply" {
			method = "POST"
		}
		rr := serve(h, withToken(method, path, pairTok))
		if rr.Code != http.StatusForbidden || bodyError(t, rr.Body.Bytes()) != "device_required" {
			t.Errorf("%s with pair token: %d %s, want 403 device_required", path, rr.Code, rr.Body.String())
		}
	}
	d := newTestDevice(t)
	pair(t, h, pairTok, d)
	devTok := deviceToken(t, h, d)
	rr := postJSON(h, "/api/qwk/devices", devTok, pairRequest{Name: "Second", PublicKey: newTestDevice(t).pub})
	if rr.Code != http.StatusForbidden || bodyError(t, rr.Body.Bytes()) != "password_login_required" {
		t.Errorf("pair with device token: %d %s, want 403 password_login_required", rr.Code, rr.Body.String())
	}
	if rr := serve(h, withToken("GET", "/api/qwk/packet", devTok)); rr.Code != http.StatusNoContent {
		t.Errorf("packet with device token: %d, want 204", rr.Code)
	}
}

func TestDevice_ListShowsCurrentAndLastUsed(t *testing.T) {
	h, _, _ := deviceServer(t)
	pairTok := passwordLogin(t, h)
	a, b := newTestDevice(t), newTestDevice(t)
	pair(t, h, pairTok, a)
	pair(t, h, pairTok, b)
	tok := deviceToken(t, h, a)
	rr := serve(h, withToken("GET", "/api/qwk/devices", tok))
	var list []deviceInfo
	_ = json.Unmarshal(rr.Body.Bytes(), &list)
	if rr.Code != http.StatusOK || len(list) != 2 {
		t.Fatalf("list: %d %s", rr.Code, rr.Body.String())
	}
	for _, d := range list {
		if d.Current != (d.ID == a.id) {
			t.Errorf("device %s current=%v", d.ID, d.Current)
		}
		if (d.LastUsed != "") != (d.ID == a.id) {
			t.Errorf("device %s lastUsed=%q", d.ID, d.LastUsed)
		}
	}
	// A pair token can list too, with nothing marked current.
	rr = serve(h, withToken("GET", "/api/qwk/devices", pairTok))
	_ = json.Unmarshal(rr.Body.Bytes(), &list)
	for _, d := range list {
		if d.Current {
			t.Error("pair token listing marked a device current")
		}
	}
}

func TestDevice_ChallengeIsSingleUseAndBound(t *testing.T) {
	h, _, g := deviceServer(t)
	pairTok := passwordLogin(t, h)
	a, b := newTestDevice(t), newTestDevice(t)
	pair(t, h, pairTok, a)
	pair(t, h, pairTok, b)

	ch := challenge(t, h, a)
	req := deviceLoginRequest{DeviceID: a.id, Challenge: ch, Signature: a.sign(ch)}
	if rr := postJSON(h, "/api/qwk/login/device", "", req); rr.Code != http.StatusOK {
		t.Fatalf("first use: %d", rr.Code)
	}
	if rr := postJSON(h, "/api/qwk/login/device", "", req); rr.Code != http.StatusUnauthorized || bodyError(t, rr.Body.Bytes()) != "challenge_invalid" {
		t.Errorf("replay: %d %s, want 401 challenge_invalid", rr.Code, rr.Body.String())
	}
	// A's challenge, used by B (correctly signed by B), is refused.
	ch = challenge(t, h, a)
	req = deviceLoginRequest{DeviceID: b.id, Challenge: ch, Signature: b.sign(ch)}
	if rr := postJSON(h, "/api/qwk/login/device", "", req); rr.Code != http.StatusUnauthorized || bodyError(t, rr.Body.Bytes()) != "challenge_invalid" {
		t.Errorf("cross-device challenge: %d %s", rr.Code, rr.Body.String())
	}
	if n := g.failed["192.0.2.1"]; n != 0 {
		t.Errorf("challenge problems counted as %d failed logins, want 0", n)
	}
}

func TestDevice_BadSignaturesFailAndCount(t *testing.T) {
	h, _, g := deviceServer(t)
	d := newTestDevice(t)
	pair(t, h, passwordLogin(t, h), d)

	// Signed by a different key.
	impostor := newTestDevice(t)
	impostor.id = d.id
	ch := challenge(t, h, d)
	rr := postJSON(h, "/api/qwk/login/device", "", deviceLoginRequest{DeviceID: d.id, Challenge: ch, Signature: impostor.sign(ch)})
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("wrong key: %d, want 401", rr.Code)
	}
	// Right key, but signed for another board.
	ch = challenge(t, h, d)
	other := base64.StdEncoding.EncodeToString(ed25519.Sign(d.priv, deviceLoginMessage("OTHERBBS", d.id, ch)))
	rr = postJSON(h, "/api/qwk/login/device", "", deviceLoginRequest{DeviceID: d.id, Challenge: ch, Signature: other})
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("other board's signature: %d, want 401", rr.Code)
	}
	// Unknown device: a challenge is still issued, so IDs can't be probed.
	ghost := newTestDevice(t)
	ghost.id = strings.Repeat("ab", 16)
	if rr := deviceLogin(t, h, ghost); rr.Code != http.StatusUnauthorized {
		t.Errorf("unknown device: %d, want 401", rr.Code)
	}
	if n := g.failed["192.0.2.1"]; n != 3 {
		t.Errorf("failed logins recorded = %d, want 3", n)
	}
}

func TestDevice_UnpairEndsItsSessions(t *testing.T) {
	h, _, _ := deviceServer(t)
	pairTok := passwordLogin(t, h)
	a, b := newTestDevice(t), newTestDevice(t)
	pair(t, h, pairTok, a)
	pair(t, h, pairTok, b)
	tokA, tokB := deviceToken(t, h, a), deviceToken(t, h, b)

	// A lost phone dropped from a new password login.
	if rr := serve(h, withToken("DELETE", "/api/qwk/devices/"+b.id, pairTok)); rr.Code != http.StatusNoContent {
		t.Fatalf("unpair b: %d %s", rr.Code, rr.Body.String())
	}
	if rr := serve(h, withToken("GET", "/api/qwk/packet", tokB)); rr.Code != http.StatusUnauthorized {
		t.Errorf("b's token after unpair: %d, want 401", rr.Code)
	}
	if rr := deviceLogin(t, h, b); rr.Code != http.StatusUnauthorized {
		t.Errorf("b login after unpair: %d, want 401", rr.Code)
	}
	if rr := serve(h, withToken("GET", "/api/qwk/packet", tokA)); rr.Code != http.StatusNoContent {
		t.Errorf("a unaffected: %d, want 204", rr.Code)
	}
	// A device unpairing itself.
	if rr := serve(h, withToken("DELETE", "/api/qwk/devices/"+a.id, tokA)); rr.Code != http.StatusNoContent {
		t.Fatalf("unpair self: %d", rr.Code)
	}
	if rr := serve(h, withToken("GET", "/api/qwk/packet", tokA)); rr.Code != http.StatusUnauthorized {
		t.Errorf("a's token after self-unpair: %d, want 401", rr.Code)
	}
	if rr := serve(h, withToken("DELETE", "/api/qwk/devices/"+a.id, pairTok)); rr.Code != http.StatusNotFound {
		t.Errorf("unpair twice: %d, want 404", rr.Code)
	}
	if rr := serve(h, withToken("GET", "/api/qwk/devices/"+a.id, pairTok)); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET on a device: %d, want 405", rr.Code)
	}
}

// Someone with a stolen password could pair their own device, so changing the
// password must void every device paired under it.
func TestDevice_PasswordChangeVoidsDevices(t *testing.T) {
	h, accounts, _ := deviceServer(t)
	d := newTestDevice(t)
	pair(t, h, passwordLogin(t, h), d)
	deviceToken(t, h, d)
	accounts.change(7, func(u *user.User) { u.PasswordHash = "hash-2" })
	if rr := deviceLogin(t, h, d); rr.Code != http.StatusUnauthorized {
		t.Errorf("device login after password change: %d, want 401", rr.Code)
	}
	rr := serve(h, withToken("GET", "/api/qwk/devices", passwordLogin(t, h)))
	if strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Errorf("devices after password change = %s, want []", rr.Body.String())
	}
}

func TestDevice_DeniedAccountCannotLogIn(t *testing.T) {
	h, accounts, _ := deviceServer(t)
	d := newTestDevice(t)
	pair(t, h, passwordLogin(t, h), d)
	accounts.change(7, func(u *user.User) { u.AccessLevel = 0 })
	rr := deviceLogin(t, h, d)
	if rr.Code != http.StatusForbidden || bodyError(t, rr.Body.Bytes()) != DenyLogonLevel {
		t.Errorf("banned device login: %d %s, want 403 logon_level", rr.Code, rr.Body.String())
	}
}

func TestDevice_PairingLimits(t *testing.T) {
	h, _, _ := deviceServer(t)
	pairTok := passwordLogin(t, h)
	first := newTestDevice(t)
	pair(t, h, pairTok, first)
	if rr := postJSON(h, "/api/qwk/devices", pairTok, pairRequest{PublicKey: first.pub}); rr.Code != http.StatusConflict || bodyError(t, rr.Body.Bytes()) != "key_exists" {
		t.Errorf("same key twice: %d %s", rr.Code, rr.Body.String())
	}
	for i := 1; i < maxDevicesPerUser; i++ {
		pair(t, h, pairTok, newTestDevice(t))
	}
	if rr := postJSON(h, "/api/qwk/devices", pairTok, pairRequest{PublicKey: newTestDevice(t).pub}); rr.Code != http.StatusConflict || bodyError(t, rr.Body.Bytes()) != "too_many_devices" {
		t.Errorf("over the cap: %d %s", rr.Code, rr.Body.String())
	}
}

func TestDevice_KeyTypes(t *testing.T) {
	p256, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 1024)
	enc := func(pub any) string {
		der, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(der)
	}
	if _, _, err := parseDeviceKey(enc(&p256.PublicKey)); err != nil {
		t.Errorf("P-256 rejected: %v", err)
	}
	if _, _, err := parseDeviceKey(newTestDevice(t).pub); err != nil {
		t.Errorf("Ed25519 rejected: %v", err)
	}
	for name, k := range map[string]string{"P-384": enc(&p384.PublicKey), "RSA": enc(&rsaKey.PublicKey), "garbage": "bm90IGEga2V5", "not base64": "%%%"} {
		if _, _, err := parseDeviceKey(k); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// P-256 signatures verify in both encodings phones produce.
func TestDevice_P256SignatureEncodings(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	msg := deviceLoginMessage(testBoardID, "dev", "chal")
	digest := sha256.Sum256(msg)
	der, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 64)
	r.FillBytes(raw[:32])
	s.FillBytes(raw[32:])
	if !verifyDeviceSignature(&key.PublicKey, msg, der) {
		t.Error("DER signature rejected")
	}
	if !verifyDeviceSignature(&key.PublicKey, msg, raw) {
		t.Error("raw r||s signature rejected")
	}
	if verifyDeviceSignature(&key.PublicKey, deviceLoginMessage(testBoardID, "dev", "other"), der) {
		t.Error("signature accepted for a different message")
	}
}

func TestCleanDeviceName(t *testing.T) {
	cases := map[string]string{
		"  Robbie's phone ":      "Robbie's phone",
		"":                       "Unnamed device",
		"evil\x1b[31mname\n":     "evil[31mname",
		strings.Repeat("x", 100): strings.Repeat("x", maxDeviceNameLen),
		strings.Repeat("é", 100): strings.Repeat("é", maxDeviceNameLen),
	}
	for in, want := range cases {
		if got := cleanDeviceName(in); got != want {
			t.Errorf("cleanDeviceName(%q) = %q, want %q", in, got, want)
		}
	}
}

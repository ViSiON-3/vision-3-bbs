package protocol

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

func testListing(t *testing.T, ks *keystore.Keystore) *CentralListing {
	t.Helper()
	now := time.Now().UTC()
	l := &CentralListing{
		V3NetCentral: CentralListingVersion,
		Network:      "testnet",
		Seq:          now.UnixNano(),
		Name:         "Sector 7",
		Description:  "Demoscene and code",
		Addresses:    []string{"ssh://sector7.example:2222", "telnet://sector7.example:23"},
		Capabilities: []string{FeatureCentralDirectory},
		Software:     "ViSiON/3 test",
		PublishedAt:  now.Format(time.RFC3339),
		ExpiresAt:    now.Add(24 * time.Hour).Format(time.RFC3339),
	}
	if err := l.Sign(ks); err != nil {
		t.Fatalf("sign: %v", err)
	}
	return l
}

func testKS(t *testing.T) *keystore.Keystore {
	t.Helper()
	ks, _, err := keystore.Load(filepath.Join(t.TempDir(), "node.key"))
	if err != nil {
		t.Fatalf("keystore: %v", err)
	}
	return ks
}

func TestCentralListingSignAndVerify(t *testing.T) {
	ks := testKS(t)
	l := testListing(t, ks)
	if l.NodeID != ks.NodeID() || l.OriginPubKeyB64 != ks.PubKeyBase64() {
		t.Fatalf("Sign did not fill node identity: %+v", l)
	}
	if err := l.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := l.Validate("testnet"); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestCentralListingVerifyRejectsAlteredFields(t *testing.T) {
	ks := testKS(t)
	for name, alter := range map[string]func(*CentralListing){
		"name":        func(l *CentralListing) { l.Name = "Not Sector 7" },
		"description": func(l *CentralListing) { l.Description += "!" },
		"address":     func(l *CentralListing) { l.Addresses[0] = "ssh://evil.example:22" },
		"capability":  func(l *CentralListing) { l.Capabilities = append(l.Capabilities, "central.presence") },
		"seq":         func(l *CentralListing) { l.Seq++ },
		"expires":     func(l *CentralListing) { l.ExpiresAt = time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339) },
		"network":     func(l *CentralListing) { l.Network = "othernet" },
		"node":        func(l *CentralListing) { l.NodeID = "0000000000000000" },
	} {
		l := testListing(t, ks)
		alter(l)
		if err := l.Verify(); err == nil {
			t.Errorf("%s: altered listing verified", name)
		}
	}
}

func TestCentralListingVerifyRejectsForeignKey(t *testing.T) {
	ks, other := testKS(t), testKS(t)
	l := testListing(t, ks)
	// A listing signed by one key but claiming another node's ID.
	l.NodeID = other.NodeID()
	if err := l.Verify(); !errors.Is(err, ErrCentralNodeMismatch) {
		t.Errorf("want ErrCentralNodeMismatch, got %v", err)
	}
	l = testListing(t, ks)
	l.SignatureB64 = ""
	if err := l.Verify(); !errors.Is(err, ErrCentralSignature) {
		t.Errorf("missing signature: want ErrCentralSignature, got %v", err)
	}
	l.OriginPubKeyB64 = ""
	if err := l.Verify(); !errors.Is(err, ErrCentralUnsigned) {
		t.Errorf("unsigned: want ErrCentralUnsigned, got %v", err)
	}
}

func TestCentralListingNilAndEmptySlicesSignTheSame(t *testing.T) {
	ks := testKS(t)
	l := testListing(t, ks)
	l.Addresses, l.Capabilities = nil, nil
	if err := l.Sign(ks); err != nil {
		t.Fatal(err)
	}
	l.Addresses, l.Capabilities = []string{}, []string{}
	if err := l.Verify(); err != nil {
		t.Errorf("empty slices should verify as nil did: %v", err)
	}
}

func TestCentralListingValidateLimits(t *testing.T) {
	ks := testKS(t)
	now := time.Now().UTC()
	cases := map[string]func(*CentralListing){
		"version":          func(l *CentralListing) { l.V3NetCentral = "2.0" },
		"network":          func(l *CentralListing) { l.Network = "othernet" },
		"no name":          func(l *CentralListing) { l.Name = "  " },
		"long name":        func(l *CentralListing) { l.Name = strings.Repeat("x", MaxCentralNameLen+1) },
		"pipe in name":     func(l *CentralListing) { l.Name = "|04Red|07" },
		"long desc":        func(l *CentralListing) { l.Description = strings.Repeat("x", MaxCentralDescriptionLen+1) },
		"control in desc":  func(l *CentralListing) { l.Description = "bad\x1b[2J" },
		"too many addrs":   func(l *CentralListing) { l.Addresses = make([]string, MaxCentralAddresses+1) },
		"bad scheme":       func(l *CentralListing) { l.Addresses = []string{"gopher://x.example"} },
		"no host":          func(l *CentralListing) { l.Addresses = []string{"ssh://"} },
		"userinfo":         func(l *CentralListing) { l.Addresses = []string{"ssh://user@x.example"} },
		"space in addr":    func(l *CentralListing) { l.Addresses = []string{"telnet://x.example:23 now"} },
		"too many caps":    func(l *CentralListing) { l.Capabilities = make([]string, MaxCentralCapabilities+1) },
		"bad cap":          func(l *CentralListing) { l.Capabilities = []string{"Central Dir"} },
		"long software":    func(l *CentralListing) { l.Software = strings.Repeat("x", MaxCentralSoftwareLen+1) },
		"seq zero":         func(l *CentralListing) { l.Seq = 0 },
		"bad published":    func(l *CentralListing) { l.PublishedAt = "yesterday" },
		"expires first":    func(l *CentralListing) { l.ExpiresAt = now.Add(-time.Hour).Format(time.RFC3339) },
		"ttl too long":     func(l *CentralListing) { l.ExpiresAt = now.Add(MaxCentralListingTTL + time.Hour).Format(time.RFC3339) },
		"empty cap string": func(l *CentralListing) { l.Capabilities = []string{""} },
	}
	for name, alter := range cases {
		l := testListing(t, ks)
		alter(l)
		if err := l.Validate("testnet"); err == nil {
			t.Errorf("%s: expected Validate to fail", name)
		}
	}
	// Edge cases that must pass.
	l := testListing(t, ks)
	l.Addresses = []string{"TELNET://Mixed.Example:23", "https://x.example/bbs", "rlogin://x.example"}
	l.Capabilities = []string{"central.directory", "a-b.c9"}
	l.Description = ""
	if err := l.Validate("testnet"); err != nil {
		t.Errorf("valid listing refused: %v", err)
	}
}

func TestCentralListingExpired(t *testing.T) {
	ks := testKS(t)
	l := testListing(t, ks)
	now := time.Now()
	if l.Expired(now) {
		t.Error("fresh listing reported expired")
	}
	if !l.Expired(now.Add(25 * time.Hour)) {
		t.Error("listing past expires_at not reported expired")
	}
	l.ExpiresAt = "never"
	if !l.Expired(now) {
		t.Error("unparseable expires_at should count as expired")
	}
}

func TestEventMeta(t *testing.T) {
	m, err := NewEventMeta(CentralEventTTL)
	if err != nil {
		t.Fatal(err)
	}
	if m.EventID == "" || len(m.EventID) < 20 {
		t.Errorf("event id too short: %q", m.EventID)
	}
	m2, _ := NewEventMeta(CentralEventTTL)
	if m.EventID == m2.EventID {
		t.Error("two events share an id")
	}
	if m.Expired(time.Now()) {
		t.Error("fresh event expired")
	}
	if !m.Expired(time.Now().Add(CentralEventTTL + time.Minute)) {
		t.Error("event past ttl not expired")
	}
	if (EventMeta{}).Expired(time.Now()) {
		t.Error("event without expiry must not expire")
	}
}

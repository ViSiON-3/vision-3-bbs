package configeditor

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func TestV3NetPinFieldValidatesAndClearsPending(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(pub)
	pin, pending := "old", key
	f := v3netPinField("Coord Key", "help", 9, &pin, &pending)

	if err := f.Set("not a key"); err == nil {
		t.Error("accepted a value that is not a base64 ed25519 key")
	}
	if pin != "old" || pending != key {
		t.Errorf("a rejected value changed state: pin %q, pending %q", pin, pending)
	}

	if err := f.Set("  " + key + " "); err != nil {
		t.Fatalf("Set(valid key): %v", err)
	}
	if pin != key || pending != "" {
		t.Errorf("after accepting: pin %q, pending %q; want the key and none", pin, pending)
	}

	pending = "offered"
	if err := f.Set(""); err != nil {
		t.Fatalf("Set(blank): %v", err)
	}
	if pin != "" || pending != "" {
		t.Errorf("after blanking: pin %q, pending %q; want both blank", pin, pending)
	}

	g := v3netPendingField("Offered Coord", 11, &pending)
	if got := g.Get(); got != "(none)" {
		t.Errorf("empty pending shows %q", got)
	}
}

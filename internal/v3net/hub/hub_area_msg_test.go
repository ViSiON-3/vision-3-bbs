package hub

import (
	"testing"
)

func TestMessageStore_StoreWithAreaTag(t *testing.T) {
	h, _ := setupTestHub(t)

	ok, err := h.messages.Store("uuid-001", "testnet", "gen.general", `{"test":"data"}`)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if !ok {
		t.Fatal("expected new message to be stored")
	}
}

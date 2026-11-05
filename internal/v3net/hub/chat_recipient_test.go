package hub

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestChatPrivateRecipientResolution(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		bobRooms, otherRooms         []string
		bobStatus                    string
		foreign, explicit, wantOther bool
		handle                       string
		want                         int
	}{
		{name: "unique handle", bobRooms: []string{"lobby"}, want: http.StatusNoContent},
		{name: "same node in several rooms", bobRooms: []string{"lobby", "games"}, want: http.StatusNoContent},
		{name: "other network ignored", bobRooms: []string{"lobby"}, foreign: true, want: http.StatusNoContent},
		{name: "duplicate across rooms and nodes", bobRooms: []string{"lobby"}, otherRooms: []string{"games"}, want: http.StatusConflict},
		{name: "explicit disambiguation", bobRooms: []string{"lobby"}, otherRooms: []string{"games"}, explicit: true, want: http.StatusNoContent},
		{name: "explicit target outside rooms", explicit: true, want: http.StatusNoContent},
		{name: "registered but no joined handle", want: http.StatusNotFound},
		{name: "exact handle matching", bobRooms: []string{"lobby"}, handle: "BOB", want: http.StatusNotFound},
		{name: "missing handle", handle: "-empty-", want: http.StatusBadRequest},
		{name: "banned candidate ignored", bobRooms: []string{"lobby"}, otherRooms: []string{"games"}, bobStatus: "banned", wantOther: true, want: http.StatusNoContent},
		{name: "pending candidate ignored", bobRooms: []string{"lobby"}, otherRooms: []string{"games"}, bobStatus: "pending", wantOther: true, want: http.StatusNoContent},
		{name: "explicit inactive target refused", bobRooms: []string{"lobby"}, bobStatus: "banned", explicit: true, want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, ts, sender := setupChatTest(t)
			bob := loadTestKeystore(t, "bob.key")
			other := loadTestKeystore(t, "other.key")
			registerLeaf(t, ts, bob)
			registerLeaf(t, ts, other)
			for _, room := range tc.bobRooms {
				h.chatRooms.Join("testnet", room, bob.NodeID(), "bob")
			}
			for _, room := range tc.otherRooms {
				h.chatRooms.Join("testnet", room, other.NodeID(), "bob")
			}
			if tc.foreign {
				h.chatRooms.Join("foreign", "lobby", other.NodeID(), "bob")
			}
			if tc.bobStatus != "" {
				if err := h.subscribers.SetStatus(bob.NodeID(), "testnet", tc.bobStatus); err != nil {
					t.Fatal(err)
				}
			}
			handle := tc.handle
			if handle == "" {
				handle = "bob"
			}
			if handle == "-empty-" {
				handle = ""
			}
			node := ""
			if tc.explicit {
				node = bob.NodeID()
			}
			body := fmt.Sprintf(`{"handle":"alice","to_handle":%q,"to_node":%q,"text":"hello"}`, handle, node)
			var detail map[string]string
			var out any
			if tc.want != http.StatusNoContent {
				out = &detail
			}
			url := ts.URL + "/v3net/v1/testnet/chat/rooms/private"
			if code := sendSigned(t, sender, "POST", url, body, out); code != tc.want {
				t.Fatalf("status %d, want %d; detail %v", code, tc.want, detail)
			}
			var count int
			if err := h.db.QueryRow(`SELECT COUNT(*) FROM chat_private_history`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if tc.want == http.StatusNoContent {
				if count != 1 {
					t.Fatalf("stored %d private messages, want 1", count)
				}
				var storedNode, storedHandle string
				if err := h.db.QueryRow(`SELECT to_node,to_handle FROM chat_private_history`).Scan(&storedNode, &storedHandle); err != nil {
					t.Fatal(err)
				}
				wantNode := bob.NodeID()
				if tc.wantOther {
					wantNode = other.NodeID()
				}
				if storedNode != wantNode || storedHandle != "bob" {
					t.Fatalf("stored destination %s@%s, want bob@%s", storedHandle, storedNode, wantNode)
				}
			} else {
				if count != 0 {
					t.Fatalf("rejected message was stored: %d rows", count)
				}
				if tc.want == http.StatusConflict {
					choices := []string{"bob@" + bob.NodeID(), "bob@" + other.NodeID()}
					slices.Sort(choices)
					if !strings.Contains(detail["error"], strings.Join(choices, ", ")) {
						t.Fatalf("missing sorted disambiguation choices: %v", detail)
					}
				}
				// Invalid targets must not consume the sender's allowance.
				valid := fmt.Sprintf(`{"handle":"alice","to_handle":"sender","to_node":%q,"text":"valid"}`, sender.NodeID())
				if code := sendSigned(t, sender, "POST", url, valid, nil); code != http.StatusNoContent {
					t.Fatalf("valid send after rejection: %d", code)
				}
			}
		})
	}
}

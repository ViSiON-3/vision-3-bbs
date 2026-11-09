package hub

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// A join lists the room's members with their nodes and board names, sorted
// by handle and then node so every leaf sees the same order, beside the bare
// handles kept for leaves that predate members.
func TestChatJoin_ListsMembersWithBoards(t *testing.T) {
	_, ts, leafKS := setupChatTest(t)
	other := loadTestKeystore(t, "other.key")
	registerLeaf(t, ts, other)

	join := func(ks *keystore.Keystore, handle string) protocol.ChatJoinResponse {
		t.Helper()
		var resp protocol.ChatJoinResponse
		if code := sendSigned(t, ks, "POST", ts.URL+"/v3net/v1/testnet/chat/rooms/join",
			fmt.Sprintf(`{"room":"lobby","handle":%q}`, handle), &resp); code != http.StatusOK {
			t.Fatalf("join %s: status %d", handle, code)
		}
		return resp
	}
	join(leafKS, "Zed")
	join(other, "bob")
	join(other, "Alice")
	resp := join(leafKS, "alice")

	want := []protocol.ChatMember{
		{Handle: "Alice", Node: other.NodeID(), BBS: "Test BBS"},
		{Handle: "alice", Node: leafKS.NodeID(), BBS: "Test BBS"},
		{Handle: "bob", Node: other.NodeID(), BBS: "Test BBS"},
		{Handle: "Zed", Node: leafKS.NodeID(), BBS: "Test BBS"},
	}
	if !slices.Equal(resp.Members, want) {
		t.Errorf("members = %+v, want %+v", resp.Members, want)
	}
	if !slices.Equal(resp.Users, []string{"Alice", "alice", "bob", "Zed"}) {
		t.Errorf("users = %v, want the members' handles in the same order", resp.Users)
	}
}

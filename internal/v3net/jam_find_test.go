package v3net

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// The JAM adapter finds a message it imported, by UUID and origin node,
// through the router;
// the router reports a tag with no local area as found (there is nothing to
// write), and an area whose writer cannot look as not found.
func TestJAMFindMessage(t *testing.T) {
	mm, _, _ := newOutboundTest(t)
	adapter := NewJAMAdapter(mm, 1)
	router := NewJAMRouter()
	router.Add("fel.general", adapter)
	router.Add("fel.blind", &mockWriter{})
	var _ leaf.JAMFinder = router

	msg := protocol.Message{AreaTag: "fel.general", MsgUUID: "u-1", From: "Remote", To: "All", Subject: "s",
		Body: "from another node", DateUTC: "2026-10-24T12:00:00Z", OriginNode: "ffff"}
	if n, found, err := router.FindMessage(msg); err != nil || found || n != 0 {
		t.Errorf("before import: FindMessage = %d, %v, %v; want not found", n, found, err)
	}
	wrote, err := router.WriteMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	if n, found, err := router.FindMessage(msg); err != nil || !found || n != wrote {
		t.Errorf("after import: FindMessage = %d, %v, %v; want %d, true", n, found, err, wrote)
	}

	other := msg
	other.OriginNode = "eeee"
	if _, found, err := router.FindMessage(other); err != nil || found {
		t.Errorf("same UUID from another node: found = %v, %v; want false", found, err)
	}

	if n, found, err := router.FindMessage(protocol.Message{AreaTag: "no.such", MsgUUID: "u-1"}); err != nil || !found || n != 0 {
		t.Errorf("unknown tag: FindMessage = %d, %v, %v; want 0, true", n, found, err)
	}
	if _, found, err := router.FindMessage(protocol.Message{AreaTag: "fel.blind", MsgUUID: "u-1"}); err != nil || found {
		t.Errorf("writer that cannot look: found = %v, %v; want false", found, err)
	}
}

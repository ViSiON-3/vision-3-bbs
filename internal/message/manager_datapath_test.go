package message

import "testing"

func TestMessageManager_DataPath(t *testing.T) {
	mm := &MessageManager{dataPath: "/srv/bbs/data"}
	if got := mm.DataPath(); got != "/srv/bbs/data" {
		t.Errorf("DataPath() = %q, want %q", got, "/srv/bbs/data")
	}
}

package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestNoteV3NetKeyChangeWarnsSysopsOnce(t *testing.T) {
	e, um := newAreaFixture(t)
	e.SetStrings(config.StringsConfig{V3NetKeyChangedNotice: "%s hub offered a new %s key"})

	if n := e.NoteV3NetKeyChange(um, "felonynet", "coordinator"); n != 1 {
		t.Fatalf("queued %d notices, want 1 (the sysop)", n)
	}
	path := sysopNoticesPath(e.GetServerConfig().DataDir)
	q, _ := peekSysopNotices(path, 1)
	if len(q) != 1 || q[0].Text != "Felonynet hub offered a new coordinator key" {
		t.Fatalf("sysop queue = %+v", q)
	}
	for _, id := range []int{2, 3} {
		if q, _ := peekSysopNotices(path, id); len(q) != 0 {
			t.Errorf("user %d was warned: %+v", id, q)
		}
	}

	if n := e.NoteV3NetKeyChange(um, "felonynet", "coordinator"); n != 0 {
		t.Errorf("queued the same warning again (%d)", n)
	}
	if n := e.NoteV3NetKeyChange(um, "felonynet", "hub"); n != 1 {
		t.Errorf("hub key warning queued %d, want 1", n)
	}

	e.SetStrings(config.StringsConfig{})
	if n := e.NoteV3NetKeyChange(um, "othernet", "hub"); n != 0 {
		t.Errorf("blank string still queued %d notices", n)
	}
}

package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// An area's sponsor may delete any message in it from the reader, as a
// co-sysop can: Del is on the bar, D asks first, and Y deletes it and packs
// the base. The sponsor's handle matches whatever its case, and the help
// lists Delete.
func TestMessageReaderSponsorDeletes(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(3)
	sponsorSetSponsor(env, generalAreaID, "caller")

	r := env.runCmd("READMSGS", env.caller, "", "?DY\x1b")
	if !r.has(" Del ") {
		t.Errorf("Del should be on the sponsor's bar; output:\n%s", r.text())
	}
	if !r.has("elete Message") {
		t.Errorf("the sponsor's help should list Delete; output:\n%s", r.text())
	}
	if !r.has("Delete this message?") {
		t.Errorf("delete should be confirmed; output:\n%s", r.text())
	}
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Fatalf("GENERAL has %d messages after the sponsor's delete, want 2", n)
	}
	if got := env.mustMsg(generalAreaID, 1).Subject; got != "subj-2" {
		t.Errorf("message 1 after delete+pack = %q, want subj-2", got)
	}
}

// Sponsoring one area gives nothing in another: a caller who isn't the
// reading area's sponsor isn't offered Delete there.
func TestMessageReaderSponsorOnlyInOwnArea(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(2)
	sponsorSetSponsor(env, generalAreaID, "Someone")

	r := env.runCmd("READMSGS", env.caller, "", "?DY\x1b")
	if r.has("Delete this message?") || r.has("elete Message") || r.has(" Del ") {
		t.Errorf("a caller who doesn't sponsor GENERAL was offered delete; output:\n%s", r.text())
	}
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Errorf("GENERAL has %d messages, want 2", n)
	}
}

// A sponsor the sysop removes while the delete is being confirmed deletes
// nothing, and is told why.
func TestMessageReaderSponsorRemovedBeforeConfirm(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(2)
	sponsorSetSponsor(env, generalAreaID, "Caller")
	env.whenOutput("Delete this message?", func() { sponsorSetSponsor(env, generalAreaID, "") })

	r := env.runCmd("READMSGS", env.caller, "", "DY\x1b")
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Errorf("GENERAL has %d messages, want 2: the delete went ahead after the sponsor was removed", n)
	}
	if !r.has("You can no longer delete messages in this area.") {
		t.Errorf("no notice; output:\n%s", r.text())
	}
}

// fakeAreas is an areaLookup over a fixed set of areas.
type fakeAreas map[int]*message.MessageArea

func (f fakeAreas) GetAreaByID(id int) (*message.MessageArea, bool) {
	a, ok := f[id]
	return a, ok
}

// sponsorsArea: the sponsor of the area, whatever the case of the handle;
// nobody for an area with no sponsor, an unknown area or no caller; and
// nobody for PRIVMAIL, whose sponsor field gives no one other callers' mail.
func TestSponsorsArea(t *testing.T) {
	areas := fakeAreas{
		1: {ID: 1, Tag: "GENERAL", Sponsor: "TechGuru"},
		2: {ID: 2, Tag: "PRIVMAIL", Sponsor: "TechGuru"},
		3: {ID: 3, Tag: "TECH"},
	}
	guru := &user.User{Handle: "techguru"}
	for _, tc := range []struct {
		name string
		u    *user.User
		area int
		want bool
	}{
		{"sponsor", guru, 1, true},
		{"someone else", &user.User{Handle: "Other"}, 1, false},
		{"private mail", guru, 2, false},
		{"no sponsor", guru, 3, false},
		{"unknown area", guru, 9, false},
		{"no caller", nil, 1, false},
	} {
		if got := sponsorsArea(tc.u, areas, tc.area); got != tc.want {
			t.Errorf("%s: sponsorsArea = %v, want %v", tc.name, got, tc.want)
		}
	}
	if sponsorsArea(guru, nil, 1) {
		t.Error("no area lookup: sponsorsArea = true")
	}
}

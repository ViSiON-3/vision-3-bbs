package menu

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// newConfMgr builds a ConferenceManager over a temp conferences.json.
func newConfMgr(t *testing.T) *conference.ConferenceManager {
	t.Helper()
	dir := t.TempDir()
	data := `[
		{"id": 1, "position": 1, "tag": "LOCAL", "name": "Local Confs"},
		{"id": 2, "position": 2, "tag": "FSXNET", "name": "fsxNet"}
	]`
	if err := os.WriteFile(filepath.Join(dir, "conferences.json"), []byte(data), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cm, err := conference.NewConferenceManager(dir)
	if err != nil {
		t.Fatalf("NewConferenceManager: %v", err)
	}
	return cm
}

func TestResolveCurrentAreaTokens(t *testing.T) {
	e := &MenuExecutor{}

	// Nil user: tag None, name falls back to the passed-in name or None.
	tag, name := e.resolveCurrentAreaTokens(nil, "")
	if tag != "None" || name != "None" {
		t.Errorf("nil user = %q/%q, want None/None", tag, name)
	}
	tag, name = e.resolveCurrentAreaTokens(nil, "Lobby")
	if tag != "None" || name != "Lobby" {
		t.Errorf("nil user with name = %q/%q, want None/Lobby", tag, name)
	}

	// User with a tag but no MessageMgr: tag from user, name None.
	u := &user.User{CurrentMessageAreaTag: "GENERAL"}
	tag, name = e.resolveCurrentAreaTokens(u, "")
	if tag != "GENERAL" || name != "None" {
		t.Errorf("user tag = %q/%q, want GENERAL/None", tag, name)
	}
}

func TestResolveCurrentFileAreaTokens(t *testing.T) {
	e := &MenuExecutor{}
	tag, name := e.resolveCurrentFileAreaTokens(nil)
	if tag != "None" || name != "None" {
		t.Errorf("nil user = %q/%q, want None/None", tag, name)
	}
	u := &user.User{CurrentFileAreaTag: "UTILS"}
	tag, name = e.resolveCurrentFileAreaTokens(u)
	if tag != "UTILS" || name != "None" {
		t.Errorf("user tag = %q/%q, want UTILS/None", tag, name)
	}
}

func TestResolveFileConferencePath_Defaults(t *testing.T) {
	e := &MenuExecutor{}
	if got := e.resolveFileConferencePath(nil); got != "Local > None" {
		t.Errorf("nil user = %q, want Local > None", got)
	}
	// FileMgr nil: same default even with a user.
	if got := e.resolveFileConferencePath(&user.User{CurrentFileAreaID: 3}); got != "Local > None" {
		t.Errorf("nil FileMgr = %q, want Local > None", got)
	}
}

func TestApplyCommonTemplateTokens_GuestDefaults(t *testing.T) {
	e := &MenuExecutor{}
	in := "user=|UH alias=|ALIAS lvl=|LEVEL node=|NODE cc=|CC ccn=|CCN fc=|FC fcn=|FCN"
	got := string(e.applyCommonTemplateTokens([]byte(in), nil, 3))
	want := "user=Guest alias=Guest lvl=0 node=3 cc=None ccn=None fc=None fcn=None"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestApplyCommonTemplateTokens_UserAndConferences(t *testing.T) {
	e := &MenuExecutor{ConferenceMgr: newConfMgr(t)}
	u := &user.User{
		Handle:                  "Felonius",
		AccessLevel:             250,
		CurrentMsgConferenceID:  1,
		CurrentFileConferenceID: 2,
	}
	in := "|HANDLE l|LEVEL cc=|CC ccn=|CCN fc=|FC fcn=|FCN"
	got := string(e.applyCommonTemplateTokens([]byte(in), u, 1))
	want := "Felonius l250 cc=LOCAL ccn=Local Confs fc=FSXNET fcn=fsxNet"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}

	// Explicit user tags win over conference-derived tags.
	u.CurrentMsgConferenceTag = "MYTAG"
	got = string(e.applyCommonTemplateTokens([]byte("|CC"), u, 1))
	if got != "MYTAG" {
		t.Errorf("|CC = %q, want MYTAG", got)
	}
}

// TestApplyCommonTemplateTokens_PrefixCollisions verifies longer tokens are
// substituted before their shorter prefixes (|CFAN vs |CFA, |CAN vs |CA).
func TestApplyCommonTemplateTokens_PrefixCollisions(t *testing.T) {
	e := &MenuExecutor{}
	u := &user.User{
		CurrentMessageAreaTag: "MSGTAG",
		CurrentFileAreaTag:    "FILETAG",
	}
	got := string(e.applyCommonTemplateTokens([]byte("|CFAN;|CFA;|CAN;|CA"), u, 1))
	// File area name unknown (None), file tag FILETAG, msg name None, msg tag MSGTAG.
	want := "None;FILETAG;None;MSGTAG"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyCommonTemplateTokens_NodeAndDate(t *testing.T) {
	e := &MenuExecutor{}
	got := string(e.applyCommonTemplateTokens([]byte("|NODE |DATE |TIME"), nil, 42))
	parts := strings.SplitN(got, " ", 3)
	if len(parts) != 3 {
		t.Fatalf("unexpected output %q", got)
	}
	if parts[0] != strconv.Itoa(42) {
		t.Errorf("|NODE = %q, want 42", parts[0])
	}
	// Date format is MM/DD/YY.
	if len(parts[1]) != 8 || parts[1][2] != '/' || parts[1][5] != '/' {
		t.Errorf("|DATE = %q, want MM/DD/YY", parts[1])
	}
	if !strings.HasSuffix(parts[2], "am") && !strings.HasSuffix(parts[2], "pm") {
		t.Errorf("|TIME = %q, want h:mm am/pm", parts[2])
	}
}

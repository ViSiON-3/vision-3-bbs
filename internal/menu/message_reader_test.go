package menu

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestBuildNameWithAddr_ShortFits(t *testing.T) {
	if got := buildNameWithAddr("Bob", "21:1/100"); got != "Bob (21:1/100)" {
		t.Errorf("buildNameWithAddr = %q, want %q", got, "Bob (21:1/100)")
	}
}

func TestBuildNameWithAddr_TruncatesPreservingSuffix(t *testing.T) {
	name := strings.Repeat("X", 60)
	got := buildNameWithAddr(name, "21:1/100")
	if !strings.HasSuffix(got, " (21:1/100)") {
		t.Errorf("address suffix not preserved: %q", got)
	}
	if utf8.RuneCountInString(got) > 45 {
		t.Errorf("result %d runes, want <= 45", utf8.RuneCountInString(got))
	}
}

func TestBuildNameWithAddr_ShortNameLongAddrNoPanic(t *testing.T) {
	// Regression: a short name with a very long address forced nameMax to 3,
	// and the old byte-slice name[:3] panicked when len(name) < 3.
	longAddr := strings.Repeat("a", 50)
	got := buildNameWithAddr("Jo", longAddr) // must not panic
	if !strings.HasSuffix(got, "("+longAddr+")") {
		t.Errorf("suffix not preserved: %q", got)
	}
	if !strings.HasPrefix(got, "Jo") {
		t.Errorf("short name should be preserved as-is: %q", got)
	}
}

func TestBuildNameWithAddr_MultibyteTruncationStaysValid(t *testing.T) {
	// Multibyte name that must be truncated: result must remain valid UTF-8
	// (the old byte-slice could split a rune).
	name := strings.Repeat("é", 50) // 50 runes, 100 bytes
	got := buildNameWithAddr(name, "1:2/3")
	if !utf8.ValidString(got) {
		t.Errorf("truncation produced invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, " (1:2/3)") {
		t.Errorf("suffix not preserved: %q", got)
	}
}

func TestSaveHeaderSelection_Persists(t *testing.T) {
	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, err := um.AddUser("password", "Bob", "Real Name", "Loc")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	if err := saveHeaderSelection(um, u, 3, 1); err != nil {
		t.Fatalf("saveHeaderSelection: %v", err)
	}
	if u.MsgHdr != 3 {
		t.Errorf("in-memory MsgHdr = %d, want 3", u.MsgHdr)
	}
	got, ok := um.GetUserByID(u.ID)
	if !ok {
		t.Fatal("user not found after save")
	}
	if got.MsgHdr != 3 {
		t.Errorf("persisted MsgHdr = %d, want 3", got.MsgHdr)
	}
}

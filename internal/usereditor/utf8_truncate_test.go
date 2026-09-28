package usereditor

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

var testSGR = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// A user list row is cut and padded to the box width in characters. With the
// Group/Location column, 35 ASCII columns come first, so a 60-column box puts
// a byte cut inside a two-byte "é".
func TestUserRowCutByRune(t *testing.T) {
	const boxW = 60
	u := &user.User{ID: 1, Handle: "Handle", GroupLocation: strings.Repeat("é", 30)}
	m := Model{users: []*user.User{u}, tagged: map[*user.User]bool{}, listType: 2}
	got := testSGR.ReplaceAllString(m.renderUserRow(0, false, boxW), "")
	if !utf8.ValidString(got) {
		t.Errorf("row %q is not valid UTF-8", got)
	}
	if n := utf8.RuneCountInString(got); n != boxW {
		t.Errorf("row is %d columns, want %d: %q", n, boxW, got)
	}
}

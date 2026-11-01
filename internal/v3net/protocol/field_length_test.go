package protocol

import (
	"strings"
	"testing"
)

// #858: origin_board, tearline and origin have length limits, counted in
// characters like the other text fields.
func TestValidateLimitsOriginFields(t *testing.T) {
	for _, tc := range []struct {
		field string
		max   int
		set   func(*Message, string)
	}{
		{"origin_board", MaxOriginBoardLen, func(m *Message, v string) { m.OriginBoard = v }},
		{"tearline", MaxTearlineLen, func(m *Message, v string) { m.Tearline = v }},
		{"origin", MaxOriginLen, func(m *Message, v string) { m.Origin = v }},
	} {
		m := validMessage()
		tc.set(&m, strings.Repeat("é", tc.max))
		if err := m.Validate(); err != nil {
			t.Errorf("%s of %d characters refused: %v", tc.field, tc.max, err)
		}
		tc.set(&m, strings.Repeat("é", tc.max+1))
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), tc.field) {
			t.Errorf("%s of %d characters: err = %v, want a %s error", tc.field, tc.max+1, err, tc.field)
		}
	}
}

func TestTrimField(t *testing.T) {
	for _, tc := range []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly", 7, "exactly"},
		{"too long", 3, "too"},
		{"héllo", 2, "hé"},
		{"caf\x82\x82", 4, "caf\x82"}, // raw CP437: one character per byte
	} {
		if got := TrimField(tc.in, tc.max); got != tc.want {
			t.Errorf("TrimField(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

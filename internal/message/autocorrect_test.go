package message

import (
	"encoding/json"
	"testing"
)

// An area's autocorrect key overrides the board default; left out, or with
// no area at all, the board default applies.
func TestAreaAutocorrectOn(t *testing.T) {
	var none *MessageArea
	for _, tc := range []struct {
		json  string
		board bool
		want  bool
	}{
		{`{"tag":"A"}`, false, false},
		{`{"tag":"A"}`, true, true},
		{`{"tag":"A","autocorrect":true}`, false, true},
		{`{"tag":"A","autocorrect":false}`, true, false},
	} {
		var a MessageArea
		if err := json.Unmarshal([]byte(tc.json), &a); err != nil {
			t.Fatal(err)
		}
		if got := a.AutocorrectOn(tc.board); got != tc.want {
			t.Errorf("%s with board %v: got %v, want %v", tc.json, tc.board, got, tc.want)
		}
	}
	if !none.AutocorrectOn(true) || none.AutocorrectOn(false) {
		t.Error("a nil area does not follow the board default")
	}
}

// CloneAreas copies the autocorrect setting rather than sharing it.
func TestCloneAreasCopiesAutocorrect(t *testing.T) {
	on := true
	areas := []MessageArea{{Tag: "A", Autocorrect: &on}}
	out := CloneAreas(areas)
	*out[0].Autocorrect = false
	if !*areas[0].Autocorrect {
		t.Error("clone shares the Autocorrect pointer")
	}
}

package user

import (
	"encoding/json"
	"testing"
)

func TestValidColorCode(t *testing.T) {
	for code, want := range map[string]bool{
		"|00": true, "|07": true, "|15": true, "|14|B1": true, "|00|B7": true,
		"|07|B8": true, "|07|B10": true, "|15|B15": true,
		"": false, "|16": false, "|23": false, "|B1": false, "|07|B16": false, "|07|23": false,
		"|07|07": false, "|23|B1": false, "|23|B10": false, "|7": false, "07": false, "|CL": false, "|C1": false, "|07|B1x": false,
	} {
		if got := ValidColorCode(code); got != want {
			t.Errorf("ValidColorCode(%q) = %v, want %v", code, got, want)
		}
	}
}

// Unset is distinct from black: a record without colorCodes, or with the old
// all-zero colors array, has no slot set, and a stored |00 is a real black.
func TestColorCodeUnsetIsNotBlack(t *testing.T) {
	var u User
	if err := json.Unmarshal([]byte(`{"handle":"x","colors":[0,0,0,0,0,0,0]}`), &u); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= ColorSlots; n++ {
		if c := u.ColorCode(n); c != "" {
			t.Fatalf("slot %d = %q on a record that never set one", n, c)
		}
	}
	if !u.SetColorCode(3, "|00") || u.ColorCode(3) != "|00" {
		t.Fatalf("slot 3 = %q, want |00", u.ColorCode(3))
	}
	data, _ := json.Marshal(u)
	var back User
	if err := json.Unmarshal(data, &back); err != nil || back.ColorCode(3) != "|00" || back.ColorCode(1) != "" {
		t.Fatalf("after a round trip: slot 3 %q, slot 1 %q (%v)", back.ColorCode(3), back.ColorCode(1), err)
	}
}

func TestSetColorCode(t *testing.T) {
	var u User
	if u.SetColorCode(0, "|07") || u.SetColorCode(8, "|07") || u.SetColorCode(1, "|CL") {
		t.Fatal("took a slot or code it can't hold")
	}
	u.SetColorCode(7, "|15|B1")
	if len(u.ColorCodes) != 7 || u.ColorCode(7) != "|15|B1" {
		t.Fatalf("ColorCodes = %q", u.ColorCodes)
	}
	u.SetColorCode(7, "")
	if u.ColorCodes != nil {
		t.Fatalf("ColorCodes = %q, want none once every slot is back to the default", u.ColorCodes)
	}
	u.ColorCodes = []string{"|CL"}
	if u.ColorCode(1) != "" {
		t.Fatal("an invalid stored code counted as set")
	}
}

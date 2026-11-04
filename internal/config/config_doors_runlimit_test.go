package config

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

// TestScriptRunLimit maps max_run_minutes onto the engine's MaxRunTime: 0 is
// the engine default, any negative value is no limit, minutes otherwise.
func TestScriptRunLimit(t *testing.T) {
	for _, tc := range []struct {
		minutes int
		want    time.Duration
	}{
		{0, 0},
		{45, 45 * time.Minute},
		{240, 4 * time.Hour},
		{-1, -1},
		{-5, -1},
		{MaxScriptRunMinutes, MaxScriptRunMinutes * time.Minute},
		// a hand-edited doors.json past the editor's maximum: capped, so the
		// duration can't overflow into "no limit" or a tiny limit
		{MaxScriptRunMinutes + 1, MaxScriptRunMinutes * time.Minute},
		{math.MaxInt, MaxScriptRunMinutes * time.Minute},
	} {
		d := DoorConfig{Code: "VAULT", Type: "v3_script", MaxRunMinutes: tc.minutes}
		if got := d.ScriptRunLimit(); got != tc.want {
			t.Errorf("max_run_minutes %d: ScriptRunLimit = %v, want %v", tc.minutes, got, tc.want)
		}
	}
}

// TestMaxRunMinutesJSON checks the doors.json key, and that an unset limit is
// left out when the config editor saves the file.
func TestMaxRunMinutesJSON(t *testing.T) {
	var d DoorConfig
	if err := json.Unmarshal([]byte(`{"code":"VAULT","type":"v3_script","max_run_minutes":-1}`), &d); err != nil {
		t.Fatal(err)
	}
	if d.MaxRunMinutes != -1 {
		t.Errorf("MaxRunMinutes = %d, want -1", d.MaxRunMinutes)
	}
	out, err := json.Marshal(DoorConfig{Code: "HELLO", Type: "v3_script"})
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]any
	if err := json.Unmarshal(out, &keys); err != nil {
		t.Fatal(err)
	}
	if _, ok := keys["max_run_minutes"]; ok {
		t.Errorf("unset limit written to doors.json: %s", out)
	}
}

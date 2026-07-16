package configeditor

import (
	"reflect"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestCSVSliceRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  []string
		asCSV string
	}{
		{"empty", "", nil, ""},
		{"whitespace only", "   ", nil, ""},
		{"single", "a", []string{"a"}, "a"},
		{"trims entries", " a , b ,c ", []string{"a", "b", "c"}, "a, b, c"},
		{"drops empty entries", "a,,b,", []string{"a", "b"}, "a, b"},
		{"all empty entries", ",,,", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := csvToSlice(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("csvToSlice(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
			if s := sliceToCSV(got); s != tt.asCSV {
				t.Errorf("sliceToCSV = %q, want %q", s, tt.asCSV)
			}
		})
	}
}

func TestDoorCommandsGetSet(t *testing.T) {
	tests := []struct {
		name     string
		isDOS    bool
		val      string
		wantCmds []string
		wantGet  string
	}{
		{"native empty", false, "", nil, ""},
		{"native command only", false, "lord", []string{"lord"}, "lord"},
		{"native with args", false, "lord /n1, /p2", []string{"lord", "/n1", "/p2"}, "lord /n1, /p2"},
		{"dos batch lines", true, "cd door, run.bat", []string{"cd door", "run.bat"}, "cd door, run.bat"},
		{"dos empty", true, "", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &doorEditProxy{IsDOS: tt.isDOS}
			doorCommandsSet(d, tt.val)
			if !reflect.DeepEqual(d.Commands, tt.wantCmds) {
				t.Errorf("Commands = %#v, want %#v", d.Commands, tt.wantCmds)
			}
			if got := doorCommandsGet(d); got != tt.wantGet {
				t.Errorf("doorCommandsGet = %q, want %q", got, tt.wantGet)
			}
		})
	}
}

func TestEnvMapCSVRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    map[string]string
		wantErr bool
	}{
		{"empty", "", nil, false},
		{"single", "A=1", map[string]string{"A": "1"}, false},
		{"multiple sorted", "B=2, A=1", map[string]string{"A": "1", "B": "2"}, false},
		{"value with equals", "URL=http://x?a=b", map[string]string{"URL": "http://x?a=b"}, false},
		{"missing value", "JUSTKEY", nil, true},
		{"empty key", "=v", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := csvToEnvMap(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("csvToEnvMap(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("csvToEnvMap(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}

	// envMapToCSV sorts keys for stable output.
	if got := envMapToCSV(map[string]string{"B": "2", "A": "1"}); got != "A=1, B=2" {
		t.Errorf("envMapToCSV = %q, want A=1, B=2", got)
	}
	if got := envMapToCSV(nil); got != "" {
		t.Errorf("envMapToCSV(nil) = %q, want empty", got)
	}
}

func TestDoorTypeLabel(t *testing.T) {
	tests := []struct {
		name string
		door config.DoorConfig
		want string
	}{
		{"syncjs", config.DoorConfig{Type: "synchronet_js"}, "SyncJS"},
		{"vpl", config.DoorConfig{Type: "v3_script"}, "VPL"},
		{"dos", config.DoorConfig{IsDOS: true}, "DOS"},
		{"native", config.DoorConfig{}, "Native"},
	}
	for _, tt := range tests {
		if got := doorTypeLabel(&tt.door); got != tt.want {
			t.Errorf("%s: doorTypeLabel = %q, want %q", tt.name, got, tt.want)
		}
	}
	if !isSyncJS(&doorEditProxy{Type: "synchronet_js"}) || isSyncJS(&doorEditProxy{}) {
		t.Error("isSyncJS misclassified")
	}
	if !isV3Script(&doorEditProxy{Type: "v3_script"}) || isV3Script(&doorEditProxy{}) {
		t.Error("isV3Script misclassified")
	}
}

func TestJoinSplitArgs(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{"empty", "", []string{}, false},
		{"json array", `["a","b c"]`, []string{"a", "b c"}, false},
		{"legacy space split", "a b c", []string{"a", "b", "c"}, false},
		{"malformed json rejected", `["a",`, nil, true},
		{"stray quote rejected", `"abc`, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := splitArgs(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("splitArgs(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitArgs(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}

	// joinArgs -> splitArgs is lossless, including spaces and quotes.
	args := []string{"-v", "arg with space", `quo"ted`}
	back, err := splitArgs(joinArgs(args))
	if err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if !reflect.DeepEqual(back, args) {
		t.Errorf("round-trip = %#v, want %#v", back, args)
	}
	if joinArgs(nil) != "" {
		t.Error("joinArgs(nil) should be empty")
	}
}

func TestSkipToColAndMaxInt(t *testing.T) {
	if got := skipToCol("abcdef", 2); got != "cdef" {
		t.Errorf("skipToCol plain = %q, want cdef", got)
	}
	if got := skipToCol("ab", 5); got != "" {
		t.Errorf("skipToCol past end = %q, want empty", got)
	}
	// ANSI escapes are replayed so styling survives the split point.
	styled := "\x1b[31mred\x1b[0mplain"
	if got := skipToCol(styled, 3); got != "\x1b[0mplain" {
		t.Errorf("skipToCol styled = %q, want %q", got, "\x1b[0mplain")
	}
	if maxInt(2, 3) != 3 || maxInt(5, -1) != 5 {
		t.Error("maxInt wrong")
	}
}

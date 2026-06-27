package configeditor

import (
	"reflect"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{"empty", "", []string{}, false},
		{"whitespace only", "   ", []string{}, false},
		{"json array", `["-x","a b"]`, []string{"-x", "a b"}, false},
		{"space fallback", "-x -y z", []string{"-x", "-y", "z"}, false},
		{"malformed json rejected", `["unterminated`, nil, true},
		{"json object rejected", `{"a":1}`, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := splitArgs(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("splitArgs(%q) expected error, got %v", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitArgs(%q) unexpected error: %v", tc.in, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitArgs(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

func TestJoinSplitArgs_RoundTrip(t *testing.T) {
	// joinArgs encodes JSON; splitArgs must recover the exact slice, including
	// args that contain spaces (which space-splitting would mangle).
	for _, args := range [][]string{
		{"-baud", "38400"},
		{"arg with spaces", "second"},
		{"--flag=value"},
	} {
		encoded := joinArgs(args)
		got, err := splitArgs(encoded)
		if err != nil {
			t.Fatalf("splitArgs(joinArgs(%v)) error: %v", args, err)
		}
		if !reflect.DeepEqual(got, args) {
			t.Errorf("round-trip of %#v = %#v (via %q)", args, got, encoded)
		}
	}
}

func TestJoinArgs_Empty(t *testing.T) {
	if got := joinArgs(nil); got != "" {
		t.Errorf("joinArgs(nil) = %q, want empty", got)
	}
}

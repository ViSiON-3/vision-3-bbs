package protocol

import (
	"strings"
	"testing"
)

func TestValidNetworkName(t *testing.T) {
	for name, want := range map[string]bool{
		"felonynet": true, "fsx_net": true, "a-b-1": true,
		"": false, "Upper": false, "bad|CL": false, "../x": false, "a b": false,
		strings.Repeat("a", 33): false,
	} {
		if got := ValidNetworkName(name); got != want {
			t.Errorf("ValidNetworkName(%q) = %v, want %v", name, got, want)
		}
	}
}

// A board name a sysop configured with colour codes or stray controls goes to
// the hub cleaned, and the hub accepts what CleanBBSName returns.
func TestCleanBBSNamePassesValidation(t *testing.T) {
	for in, want := range map[string]string{
		"Cool BBS":              "Cool BBS",
		"|09Cool |15BBS":        "/09Cool /15BBS",
		"Tab\there\x1b[2J":      "Tab here [2J",
		"C1\u009bname":          "C1 name",
		strings.Repeat("é", 70): strings.Repeat("é", MaxBBSNameLen),
	} {
		got := CleanBBSName(in)
		if got != want {
			t.Errorf("CleanBBSName(%q) = %q, want %q", in, got, want)
		}
		if err := ValidateBBSName(got); err != nil {
			t.Errorf("ValidateBBSName(CleanBBSName(%q)) = %v", in, err)
		}
	}
}

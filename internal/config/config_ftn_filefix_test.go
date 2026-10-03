package config

import (
	"encoding/json"
	"testing"
)

// The FileFix fields survive a load: UnmarshalJSON copies fields one by one,
// so a new field it forgets is silently dropped.
func TestFTNLinkConfigFilefixFieldsUnmarshal(t *testing.T) {
	var l FTNLinkConfig
	if err := json.Unmarshal([]byte(`{"address":"21:1/100","filefix_password":"ffpw","filefix_name":"AllFix"}`), &l); err != nil {
		t.Fatal(err)
	}
	if l.FilefixPassword != "ffpw" || l.FilefixName != "AllFix" {
		t.Fatalf("got password %q name %q", l.FilefixPassword, l.FilefixName)
	}
}

func TestFTNLinkConfigFilefixDefaults(t *testing.T) {
	for _, tc := range []struct {
		name          string
		link          FTNLinkConfig
		robot, passwd string
	}{
		{"unset", FTNLinkConfig{}, "FileFix", ""},
		{"tic fallback", FTNLinkConfig{TICPassword: "tic"}, "FileFix", "tic"},
		{"own password wins", FTNLinkConfig{TICPassword: "tic", FilefixPassword: "ff"}, "FileFix", "ff"},
		{"robot name", FTNLinkConfig{FilefixName: " AllFix "}, "AllFix", ""},
		{"blank robot name", FTNLinkConfig{FilefixName: "  "}, "FileFix", ""},
	} {
		if got := tc.link.FilefixRobot(); got != tc.robot {
			t.Errorf("%s: FilefixRobot() = %q, want %q", tc.name, got, tc.robot)
		}
		if got := tc.link.FilefixPass(); got != tc.passwd {
			t.Errorf("%s: FilefixPass() = %q, want %q", tc.name, got, tc.passwd)
		}
	}
}

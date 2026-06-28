package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestCompactFileSize(t *testing.T) {
	cases := []struct {
		size int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1023, "1023B"},
		{1024, "1k"},
		{2048, "2k"},
		{10*1024*1024 - 1, "10239k"},
		{10 * 1024 * 1024, "10M"},
		{25 * 1024 * 1024, "25M"},
	}
	for _, tc := range cases {
		if got := compactFileSize(tc.size); got != tc.want {
			t.Errorf("compactFileSize(%d) = %q, want %q", tc.size, got, tc.want)
		}
	}
}

func hotkeys(entries []cmdEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.hotkey
	}
	return out
}

func TestBuildFileListCmdBar_Defaults(t *testing.T) {
	e := &MenuExecutor{ServerCfg: config.ServerConfig{CoSysOpLevel: 100}}
	regular := &user.User{Handle: "Reg", AccessLevel: 10}

	cmd, sysop, userBar, _, isSysop := buildFileListCmdBar(e, regular, nil, nil)

	if isSysop {
		t.Error("regular user (level 10 < CoSysOp 100) should not be sysop")
	}
	if len(sysop) != 0 {
		t.Errorf("non-sysop should get no sysop entries, got %d", len(sysop))
	}
	gotHK := hotkeys(cmd)
	wantHK := []string{" ", "i", "v", "d", "u", "q"}
	if len(gotHK) != len(wantHK) {
		t.Fatalf("default cmd bar = %v, want %v", gotHK, wantHK)
	}
	for i := range wantHK {
		if gotHK[i] != wantHK[i] {
			t.Errorf("default hotkey[%d] = %q, want %q", i, gotHK[i], wantHK[i])
		}
	}
	// userEntries is a copy of the user bar.
	if len(userBar) != len(cmd) {
		t.Errorf("userEntries len = %d, want %d", len(userBar), len(cmd))
	}
}

func TestBuildFileListCmdBar_SysopEntries(t *testing.T) {
	e := &MenuExecutor{ServerCfg: config.ServerConfig{CoSysOpLevel: 100}}
	sysopUser := &user.User{Handle: "Sys", AccessLevel: 255}

	_, sysop, _, _, isSysop := buildFileListCmdBar(e, sysopUser, nil, nil)
	if !isSysop {
		t.Fatal("level-255 user should be a sysop with CoSysOp 100")
	}
	wantHK := []string{"e", "k", "m", "r"}
	gotHK := hotkeys(sysop)
	if len(gotHK) != len(wantHK) {
		t.Fatalf("sysop bar = %v, want %v", gotHK, wantHK)
	}
	for i := range wantHK {
		if gotHK[i] != wantHK[i] {
			t.Errorf("sysop hotkey[%d] = %q, want %q", i, gotHK[i], wantHK[i])
		}
	}
}

func TestBuildFileListCmdBar_UsesConfiguredOptions(t *testing.T) {
	e := &MenuExecutor{ServerCfg: config.ServerConfig{CoSysOpLevel: 100}}
	regular := &user.User{Handle: "Reg", AccessLevel: 10}
	opts := []LightbarOption{
		{Text: "Go", HotKey: "G"},
		{Text: "Back", HotKey: "B"},
	}
	cmd, _, _, _, _ := buildFileListCmdBar(e, regular, opts, nil)
	if len(cmd) != 2 {
		t.Fatalf("configured cmd bar = %d entries, want 2", len(cmd))
	}
	// HotKeys are lowercased.
	if cmd[0].hotkey != "g" || cmd[1].hotkey != "b" {
		t.Errorf("configured hotkeys = %q/%q, want g/b", cmd[0].hotkey, cmd[1].hotkey)
	}
	if cmd[0].label != "Go" {
		t.Errorf("label = %q, want Go", cmd[0].label)
	}
}

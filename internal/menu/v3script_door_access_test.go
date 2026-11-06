package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// A script may download from an area only if the caller passes both its
// list ACS (a script can name an area the caller cannot see) and its
// download ACS.
func TestV3ScriptCanDownload(t *testing.T) {
	ctx := &DoorCtx{CurrentUser: &user.User{ID: 7, Handle: "Neo", AccessLevel: 50}, SessionStartTime: time.Now()}
	can := v3ScriptCanDownload(ctx)
	for _, tc := range []struct {
		name string
		area file.FileArea
		want bool
	}{
		{"open area", file.FileArea{}, true},
		{"list ACS passed", file.FileArea{ACSList: "s50"}, true},
		{"hidden by list ACS, no download ACS", file.FileArea{ACSList: "s100"}, false},
		{"hidden by list ACS, download ACS passed", file.FileArea{ACSList: "s100", ACSDownload: "s10"}, false},
		{"download ACS failed", file.FileArea{ACSDownload: "s100"}, false},
		{"both passed", file.FileArea{ACSList: "s10", ACSDownload: "s50"}, true},
	} {
		if got := can(&tc.area); got != tc.want {
			t.Errorf("%s: CanDownload = %v, want %v", tc.name, got, tc.want)
		}
	}
	if v3ScriptCanDownload(&DoorCtx{})(&file.FileArea{}) {
		t.Error("CanDownload with no caller = true, want false")
	}
}

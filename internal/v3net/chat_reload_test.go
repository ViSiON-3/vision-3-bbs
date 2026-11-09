package v3net

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// Chat sessions on a leaf a reload stops are told what happened: the
// board left the network, chat was turned off, or only the connection
// was reset.
func TestChatCloseReason(t *testing.T) {
	chatOff := false
	on := config.V3NetLeafConfig{Central: &config.V3NetCentralConfig{Enabled: true}}
	off := config.V3NetLeafConfig{Central: &config.V3NetCentralConfig{Enabled: true, Chat: &chatOff}}
	for name, tc := range map[string]struct {
		next config.V3NetLeafConfig
		ok   bool
		want string
	}{
		"removed":  {config.V3NetLeafConfig{}, false, "left the network"},
		"chat off": {off, true, "no longer offered"},
		"kept":     {on, true, "/network"},
	} {
		if got := chatCloseReason(tc.next, tc.ok); !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q, want it to mention %q", name, got, tc.want)
		}
	}
}

package mailer

import (
	"reflect"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// binkd parses flags before the positional config path, so -m has to come
// first; passing it after the path makes binkd treat it as a second config
// file and abort.
func TestBinkdArgs(t *testing.T) {
	tests := []struct {
		name     string
		disable  bool
		wantArgs []string
	}{
		{"default keeps CRAM-MD5", false, []string{"/bbs/data/ftn/binkd.conf"}},
		{"disabled passes -m first", true, []string{"-m", "/bbs/data/ftn/binkd.conf"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{
				confPath: "/bbs/data/ftn/binkd.conf",
				cfg: Config{
					FTN: config.FTNConfig{
						Binkd: config.BinkdServerConfig{DisableCramMD5: tc.disable},
					},
				},
			}
			if got := s.binkdArgs(); !reflect.DeepEqual(got, tc.wantArgs) {
				t.Errorf("binkdArgs() = %v, want %v", got, tc.wantArgs)
			}
		})
	}
}

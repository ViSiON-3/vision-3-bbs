package menu

import (
	"context"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// fakeV3NetStatus is a minimal V3NetStatusProvider backed by a single
// in-memory NAL, standing in for a hub HTTP fetch in tests.
type fakeV3NetStatus struct {
	network string
	hubURL  string
	nal     *protocol.NAL
}

func (f *fakeV3NetStatus) NodeID() string                 { return "TEST" }
func (f *fakeV3NetStatus) HubActive() bool                { return false }
func (f *fakeV3NetStatus) LeafCount() int                 { return 1 }
func (f *fakeV3NetStatus) LeafNetworks() []string         { return []string{f.network} }
func (f *fakeV3NetStatus) NetworkForArea(int) string      { return f.network }
func (f *fakeV3NetStatus) HubURLForNetwork(string) string { return f.hubURL }
func (f *fakeV3NetStatus) RegistryURL() string            { return "" }
func (f *fakeV3NetStatus) FetchNALForNetwork(ctx context.Context, network string) (*protocol.NAL, error) {
	return f.nal, nil
}
func (f *fakeV3NetStatus) ProposeArea(network string, req protocol.AreaProposalRequest) (*protocol.ProposalResponse, error) {
	return nil, nil
}

// runV3NetAreas builds each row as " %s %-24s %-28s %-8s %s" (status, tag,
// name, access, network) then clamps the whole line to termWidth-1 with
// len(line) and line[:maxW] — both byte counts. tag/name are already padded
// to a fixed rune width via padRight/truncateStr, but the trailing network
// name is appended unpadded: a multi-byte network name can push the line's
// byte length over the limit even while its visible (rune) width still fits,
// triggering a truncation that lands mid-rune. Network names are hub-supplied
// (this NAL stands in for the hub's HTTP response), so this is reachable
// without local misconfiguration.
func TestRunV3NetAreasRowNotSplitMidRune(t *testing.T) {
	// 10 CJK runes = 30 bytes. Prefix (status+tag+name+access, all ASCII,
	// padded) is exactly 68 visible columns/bytes, so the full row is 78
	// visible columns (fits under maxW=79) but 98 bytes (over it).
	network := strings.Repeat("日", 10)

	nal := &protocol.NAL{
		Network: network,
		Areas: []protocol.Area{
			{
				Tag:    "aa.bb",
				Name:   "TestArea",
				Access: protocol.AreaAccess{Mode: "open"},
			},
		},
	}

	e := &MenuExecutor{
		V3NetStatus:    &fakeV3NetStatus{network: network, hubURL: "https://hub.example", nal: nal},
		RootConfigPath: t.TempDir(),
	}
	ts := newTestSession("q")
	terminal := newTestTerminal(ts)

	c := &cmdCtx{
		e: e, s: ts, terminal: terminal, currentUser: &user.User{Handle: "Tester", AccessLevel: 255},
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}

	if _, _, err := runV3NetAreas(c, ""); err != nil {
		t.Fatalf("runV3NetAreas: %v", err)
	}

	out := ts.output()
	// The item row's Network column should render the full, intact name.
	// A byte-based clamp instead truncates it mid-rune, e.g. to
	// "日日日µù" (3 intact runes plus stray CP437 glyphs from the dangling bytes).
	if !strings.Contains(out, "open     "+network) {
		t.Errorf("item row does not contain the intact network name %q after \"open\": %q", network, out)
	}
}

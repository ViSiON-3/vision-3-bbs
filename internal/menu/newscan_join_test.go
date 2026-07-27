package menu

import (
	"reflect"
	"sort"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func allReadable(*message.MessageArea) bool { return true }

func areaFix(tag, network string, autoJoin bool) *message.MessageArea {
	return &message.MessageArea{Tag: tag, Network: network, AutoJoin: autoJoin}
}

func TestBuildPlan_GrandfatherWhenSeenSetsEmpty(t *testing.T) {
	u := &user.User{Handle: "old"}
	p := buildNewscanJoinPlan([]*message.MessageArea{areaFix("GEN", "", true)}, u, allReadable)
	if !p.Grandfather {
		t.Fatal("expected grandfather for user with empty seen-sets")
	}
}

func TestBuildPlan_SilentLocalAdd(t *testing.T) {
	u := &user.User{SeenNewscanAreaTags: []string{"OLD"}}
	p := buildNewscanJoinPlan([]*message.MessageArea{
		areaFix("OLD", "", true),
		areaFix("CHAT", "", true),
		areaFix("NOJOIN", "", false),
	}, u, allReadable)
	if p.Grandfather {
		t.Fatal("unexpected grandfather")
	}
	if !reflect.DeepEqual(p.SilentTags, []string{"CHAT"}) {
		t.Errorf("SilentTags = %v, want [CHAT]", p.SilentTags)
	}
	if !reflect.DeepEqual(p.SeenTags, []string{"CHAT"}) {
		t.Errorf("SeenTags = %v, want [CHAT]", p.SeenTags)
	}
}

func TestBuildPlan_NewNetworkGrouped(t *testing.T) {
	u := &user.User{SeenNewscanAreaTags: []string{"GEN"}, SeenNewscanNetworks: []string{"fsxnet"}}
	p := buildNewscanJoinPlan([]*message.MessageArea{
		areaFix("GEN", "", true),
		areaFix("TQW_GEN", "TQWNet", true),
		areaFix("TQW_TECH", "TQWNet", true),
		areaFix("TQW_PRIVATE", "TQWNet", false),
	}, u, allReadable)
	want := map[string][]string{"tqwnet": {"TQW_GEN", "TQW_TECH"}}
	if !reflect.DeepEqual(p.NetworkTags, want) {
		t.Errorf("NetworkTags = %v, want %v", p.NetworkTags, want)
	}
	if p.NetworkNames["tqwnet"] != "TQWNet" {
		t.Errorf("NetworkNames = %v, want display name TQWNet", p.NetworkNames)
	}
	if !reflect.DeepEqual(p.SeenNets, []string{"tqwnet"}) {
		t.Errorf("SeenNets = %v, want [tqwnet]", p.SeenNets)
	}
}

func TestBuildPlan_SeenNetworkAreasMarkedSeenNotTagged(t *testing.T) {
	// User declined fsxnet earlier; a new area appears in it later.
	u := &user.User{SeenNewscanAreaTags: []string{"FSX_GEN"}, SeenNewscanNetworks: []string{"fsxnet"}}
	p := buildNewscanJoinPlan([]*message.MessageArea{
		areaFix("FSX_GEN", "fsxnet", true),
		areaFix("FSX_NEW", "fsxnet", true),
	}, u, allReadable)
	if len(p.SilentTags) != 0 || len(p.NetworkTags) != 0 {
		t.Errorf("declined network must not tag: silent=%v nets=%v", p.SilentTags, p.NetworkTags)
	}
	if !reflect.DeepEqual(p.SeenTags, []string{"FSX_NEW"}) {
		t.Errorf("SeenTags = %v, want [FSX_NEW]", p.SeenTags)
	}
}

func TestBuildPlan_InaccessibleAreasSkipped(t *testing.T) {
	u := &user.User{SeenNewscanAreaTags: []string{"X"}}
	p := buildNewscanJoinPlan([]*message.MessageArea{areaFix("SYSOP", "", true)}, u,
		func(*message.MessageArea) bool { return false })
	if len(p.SilentTags) != 0 || len(p.SeenTags) != 0 {
		t.Errorf("inaccessible area leaked into plan: %+v", p)
	}
}

func TestBuildPlan_CaseInsensitiveNetworkMatch(t *testing.T) {
	u := &user.User{SeenNewscanAreaTags: []string{"X"}, SeenNewscanNetworks: []string{"tqwnet"}}
	p := buildNewscanJoinPlan([]*message.MessageArea{areaFix("TQW_GEN", "TQWNET", true)}, u, allReadable)
	if len(p.NetworkTags) != 0 {
		t.Errorf("TQWNET should match seen 'tqwnet'; got prompt for %v", p.NetworkTags)
	}
}

func TestInitNewscanSeen(t *testing.T) {
	u := &user.User{}
	initNewscanSeen(u, []*message.MessageArea{
		areaFix("GEN", "", true),
		areaFix("PRIV", "", false),
		areaFix("FSX_GEN", "fsxnet", true),
		areaFix("TQW_GEN", "TQWNet", false), // network counts even without AutoJoin
	})
	sort.Strings(u.SeenNewscanAreaTags)
	if !reflect.DeepEqual(u.SeenNewscanAreaTags, []string{"FSX_GEN", "GEN"}) {
		t.Errorf("SeenNewscanAreaTags = %v", u.SeenNewscanAreaTags)
	}
	sort.Strings(u.SeenNewscanNetworks)
	if !reflect.DeepEqual(u.SeenNewscanNetworks, []string{"fsxnet", "tqwnet"}) {
		t.Errorf("SeenNewscanNetworks = %v", u.SeenNewscanNetworks)
	}
}

package ftn

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
)

func TestPlanFileEchoAreas(t *testing.T) {
	existing := []file.FileArea{
		{ID: 1, Tag: "GENERAL", Path: "general"},
		// Already carries TQW_NODE: left alone on a re-run.
		{ID: 7, Tag: "NODELISTS", Path: "nodes", Network: "tqwnet", FileEcho: "TQW_NODE"},
		// A local area whose tag the new one would take.
		{ID: 3, Tag: "TQW_INFO", Path: "info"},
	}
	echoes := []EchoArea{
		{Tag: "TQW_NODE", Description: "Weekly Nodelists"},
		{Tag: "TQW_INFO", Description: "Weekly Infopacks"},
		{Tag: "tqw_linuxfiles", Description: ""},
	}

	added, linked := PlanFileEchoAreas(existing, echoes, FileEchoAreaOptions{Network: "tqwnet", ACSUpload: "s250", ConferenceID: 2})

	if len(linked) != 1 || linked[0].ID != 7 {
		t.Errorf("linked = %+v, want the existing NODELISTS area", linked)
	}
	if len(added) != 2 {
		t.Fatalf("added = %+v, want 2 areas", added)
	}
	info, linux := added[0], added[1]
	if info.ID != 8 || info.Tag != "TQW_INFO_2" || info.Path != "tqwnet/tqw_info" ||
		info.Name != "Weekly Infopacks" || info.Network != "tqwnet" || info.FileEcho != "TQW_INFO" ||
		info.ACSUpload != "s250" || info.ConferenceID != 2 {
		t.Errorf("info area = %+v", info)
	}
	// No description: named after the echo; the echo tag is stored upper-cased.
	if linux.ID != 9 || linux.Name != "tqw_linuxfiles" || linux.FileEcho != "TQW_LINUXFILES" || linux.Tag != "TQW_LINUXFILES" {
		t.Errorf("linux area = %+v", linux)
	}
}

func TestPlanFileEchoAreasSkipsUnsafeTags(t *testing.T) {
	added, _ := PlanFileEchoAreas(nil, []EchoArea{{Tag: ".."}, {Tag: "a/b"}, {Tag: "OK"}}, FileEchoAreaOptions{Network: "tqwnet"})
	if len(added) != 1 || added[0].Path != "tqwnet/ok" {
		t.Errorf("added = %+v, want only OK", added)
	}
}

func TestPlanFileEchoAreasOtherNetworkIsNotLinked(t *testing.T) {
	existing := []file.FileArea{{ID: 1, Tag: "X", Path: "x", Network: "fsxnet", FileEcho: "TQW_NODE"}}
	added, linked := PlanFileEchoAreas(existing, []EchoArea{{Tag: "TQW_NODE"}}, FileEchoAreaOptions{Network: "tqwnet", TagPrefix: "t_"})
	if len(linked) != 0 || len(added) != 1 || added[0].Tag != "T_TQW_NODE" {
		t.Errorf("added %+v, linked %+v", added, linked)
	}
}

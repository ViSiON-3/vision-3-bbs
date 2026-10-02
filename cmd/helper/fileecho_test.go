package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

func TestPlanFileEchoAreas(t *testing.T) {
	existing := []file.FileArea{
		{ID: 1, Tag: "GENERAL", Path: "general"},
		// Already carries TQW_NODE: left alone on a re-run.
		{ID: 7, Tag: "NODELISTS", Path: "nodes", Network: "tqwnet", FileEcho: "TQW_NODE"},
		// A local area whose tag the new one would take.
		{ID: 3, Tag: "TQW_INFO", Path: "info"},
	}
	echoes := []ftn.EchoArea{
		{Tag: "TQW_NODE", Description: "Weekly Nodelists"},
		{Tag: "TQW_INFO", Description: "Weekly Infopacks"},
		{Tag: "tqw_linuxfiles", Description: ""},
	}

	added, linked := planFileEchoAreas(existing, echoes, fileEchoOptions{network: "tqwnet", acsUpload: "s250", conferenceID: 2})

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
	added, _ := planFileEchoAreas(nil, []ftn.EchoArea{{Tag: ".."}, {Tag: "a/b"}, {Tag: "OK"}}, fileEchoOptions{network: "tqwnet"})
	if len(added) != 1 || added[0].Path != "tqwnet/ok" {
		t.Errorf("added = %+v, want only OK", added)
	}
}

func TestCheckConference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conferences.json")
	if err := os.WriteFile(path, []byte(`[{"id":2,"tag":"T","name":"T"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkConference(path, 2); err != nil {
		t.Errorf("existing conference rejected: %v", err)
	}
	if err := checkConference(path, 9); err == nil {
		t.Error("unknown conference accepted")
	}
}

func TestPlanFileEchoAreasOtherNetworkIsNotLinked(t *testing.T) {
	existing := []file.FileArea{{ID: 1, Tag: "X", Path: "x", Network: "fsxnet", FileEcho: "TQW_NODE"}}
	added, linked := planFileEchoAreas(existing, []ftn.EchoArea{{Tag: "TQW_NODE"}}, fileEchoOptions{network: "tqwnet", tagPrefix: "t_"})
	if len(linked) != 0 || len(added) != 1 || added[0].Tag != "T_TQW_NODE" {
		t.Errorf("added %+v, linked %+v", added, linked)
	}
}

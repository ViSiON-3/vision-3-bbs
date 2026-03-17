package v3net

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

func TestSyncAreasCreatesMissing(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Start with one existing area.
	initial := []*message.MessageArea{
		{ID: 1, Position: 1, Tag: "GENERAL", Name: "General", BasePath: "msgbases/general", AreaType: "local"},
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(filepath.Join(configDir, "message_areas.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mgr, err := message.NewMessageManager(tmpDir, configDir, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	defer mgr.Close()

	leaves := []config.V3NetLeafConfig{
		{HubURL: "https://hub.example.com", Network: "felonynet", Board: "FELGEN"},
		{HubURL: "https://hub.example.com", Network: "felonynet", Board: "GENERAL"}, // already exists
		{HubURL: "https://hub.example.com", Network: "felonynet", Board: "FELTECH"},
	}

	created := SyncAreas(leaves, mgr)
	if created != 2 {
		t.Errorf("SyncAreas created = %d, want 2", created)
	}

	// Verify FELGEN was created.
	area, ok := mgr.GetAreaByTag("FELGEN")
	if !ok {
		t.Fatal("FELGEN not found")
	}
	if area.AreaType != "v3net" {
		t.Errorf("FELGEN AreaType = %q, want v3net", area.AreaType)
	}
	if area.Network != "felonynet" {
		t.Errorf("FELGEN Network = %q, want felonynet", area.Network)
	}

	// Verify FELTECH was created.
	area2, ok := mgr.GetAreaByTag("FELTECH")
	if !ok {
		t.Fatal("FELTECH not found")
	}
	if area2.ID <= area.ID {
		t.Error("FELTECH should have higher ID than FELGEN")
	}

	// Verify GENERAL was not duplicated.
	general, ok := mgr.GetAreaByID(1)
	if !ok {
		t.Fatal("GENERAL area 1 not found")
	}
	if general.AreaType != "local" {
		t.Errorf("GENERAL should still be local, got %q", general.AreaType)
	}

	// Run again — should create nothing.
	created2 := SyncAreas(leaves, mgr)
	if created2 != 0 {
		t.Errorf("second SyncAreas created = %d, want 0", created2)
	}
}

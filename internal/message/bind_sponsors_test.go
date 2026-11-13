package message

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Sponsors named only by handle are bound once, to the account holding the
// handle then, and saved; a bound sponsor is left alone, and one that names
// no account is bound to nobody so a later sign-up with that handle doesn't
// become the sponsor.
func TestBindSponsors(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMsgAreas(t, cfg, `[
		{"id":1,"position":1,"tag":"A","name":"A","sponsor":"Alice"},
		{"id":2,"position":2,"tag":"B","name":"B","sponsor":"Ghost"},
		{"id":3,"position":3,"tag":"C","name":"C","sponsor":"Bob","sponsor_id":9},
		{"id":4,"position":4,"tag":"D","name":"D"}]`)
	mm, err := NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int{"alice": 5, "bob": 6}
	idFor := func(h string) int { return ids[strings.ToLower(h)] }

	bound, unowned, err := mm.BindSponsors(idFor)
	if err != nil || bound != 1 || unowned != 1 {
		t.Fatalf("BindSponsors = %d, %d, %v", bound, unowned, err)
	}
	for id, want := range map[int]int{1: 5, 2: NoSponsorID, 3: 9, 4: 0} {
		a, _ := mm.GetAreaByID(id)
		if a.SponsorID != want {
			t.Errorf("area %d SponsorID = %d, want %d", id, a.SponsorID, want)
		}
	}
	data, _ := os.ReadFile(filepath.Join(cfg, "message_areas.json"))
	if !strings.Contains(string(data), `"sponsor_id": 5`) {
		t.Errorf("binding not saved:\n%s", data)
	}

	// The handle "Ghost" signing up later doesn't make them the sponsor.
	ids["ghost"] = 7
	if bound, unowned, _ := mm.BindSponsors(idFor); bound+unowned != 0 {
		t.Errorf("second run bound %d, %d", bound, unowned)
	}
	if a, _ := mm.GetAreaByID(2); a.SponsoredBy(7) {
		t.Error("a later account with the handle became the sponsor")
	}
}

// A binding that can't be saved is put back in memory too: the running BBS
// gives no sponsor until the file holds the binding.
func TestBindSponsorsSaveFailureLeavesAreasUnbound(t *testing.T) {
	// The save is made to fail with a read-only directory, which Windows
	// doesn't enforce and root ignores.
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions aren't enforced here")
	}
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMsgAreas(t, cfg, `[{"id":1,"position":1,"tag":"A","name":"A","sponsor":"Alice"}]`)
	mm, err := NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cfg, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfg, 0o755) })
	if _, _, err := mm.BindSponsors(func(string) int { return 5 }); err == nil {
		t.Fatal("BindSponsors saved into a read-only directory")
	}
	if a, _ := mm.GetAreaByID(1); a.SponsorID != 0 || a.SponsoredBy(5) {
		t.Errorf("after a failed save, SponsorID = %d", a.SponsorID)
	}
}

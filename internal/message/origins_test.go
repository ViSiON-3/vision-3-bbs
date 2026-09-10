package message

import (
	"sync"
	"testing"
)

func originsTestManager(t *testing.T) *MessageManager {
	t.Helper()
	dataDir, configDir := t.TempDir(), t.TempDir()
	writeMsgAreas(t, configDir, `[{"id":1,"tag":"GEN","name":"General","network":"fidonet"}]`)
	mm, err := NewMessageManager(dataDir, configDir, "InitialBoard", map[string]string{"fidonet": "Initial Origin"})
	if err != nil {
		t.Fatal(err)
	}
	return mm
}

func TestSetNetworkOriginsApplies(t *testing.T) {
	mm := originsTestManager(t)

	if got := mm.OriginTextForNetwork("fidonet"); got != "Initial Origin" {
		t.Fatalf("initial origin = %q", got)
	}

	mm.SetNetworkOrigins(map[string]string{"FidoNet ": "New Origin"}) // normalization applies
	if got := mm.OriginTextForNetwork("fidonet"); got != "New Origin" {
		t.Errorf("origin after SetNetworkOrigins = %q, want New Origin", got)
	}

	// Clearing the overrides falls back to the board name.
	mm.SetNetworkOrigins(nil)
	if got := mm.OriginTextForNetwork("fidonet"); got != "InitialBoard" {
		t.Errorf("origin after clearing = %q, want InitialBoard", got)
	}
}

func TestSetBoardNameAppliesToFallback(t *testing.T) {
	mm := originsTestManager(t)
	mm.SetNetworkOrigins(nil)

	mm.SetBoardName("Renamed BBS")
	if got := mm.OriginTextForNetwork("unlisted-net"); got != "Renamed BBS" {
		t.Errorf("fallback origin = %q, want Renamed BBS", got)
	}
}

// TestOriginSettersRaceWithReads proves the newly-mutable fields are safe
// against the post path's reads under -race.
func TestOriginSettersRaceWithReads(t *testing.T) {
	mm := originsTestManager(t)

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		for i := 0; i < 2000; i++ {
			mm.SetNetworkOrigins(map[string]string{"fidonet": "Origin A"})
			mm.SetBoardName("Board B")
		}
	}()
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				_ = mm.OriginTextForNetwork("fidonet")
				_ = mm.OriginTextForNetwork("other")
			}
		}()
	}
	wg.Wait()
}

package message

import (
	"strings"
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

// TestEchomailPostCarriesOrigin posts real echomail through addMessage's
// WriteMessageExt path and reads the JAM text back, so the integration point
// between the manager and the origin line is covered — not just the
// OriginTextForNetwork accessor. It then updates the origins and the board
// name and posts again, proving the hot-reload path reaches actual messages.
func TestEchomailPostCarriesOrigin(t *testing.T) {
	dataDir, configDir := t.TempDir(), t.TempDir()
	// origin_addr matters: the JAM writer only appends " * Origin:" when the
	// area has an FTN address to stamp it with.
	writeMsgAreas(t, configDir, `[{"id":1,"tag":"ECHO","name":"Echo","area_type":"echomail","echo_tag":"TEST_ECHO","network":"fidonet","origin_addr":"21:3/110"}]`)
	mm, err := NewMessageManager(dataDir, configDir, "Fallback Board", map[string]string{"fidonet": "Configured Origin"})
	if err != nil {
		t.Fatal(err)
	}

	postAndRead := func(subject string) string {
		t.Helper()
		msgNum, err := mm.AddMessage(1, "Alice", "All", subject, "hello there", "")
		if err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
		msg, err := mm.GetMessage(1, msgNum)
		if err != nil {
			t.Fatalf("GetMessage: %v", err)
		}
		return msg.Body
	}

	if body := postAndRead("first"); !strings.Contains(body, "Configured Origin") {
		t.Errorf("posted echomail body missing configured origin:\n%s", body)
	}

	mm.SetNetworkOrigins(nil) // drop the override: fallback takes over
	if body := postAndRead("second"); !strings.Contains(body, "Fallback Board") {
		t.Errorf("posted echomail body missing board-name fallback:\n%s", body)
	}

	mm.SetBoardName("Renamed Board")
	if body := postAndRead("third"); !strings.Contains(body, "Renamed Board") {
		t.Errorf("posted echomail body missing renamed fallback:\n%s", body)
	}
}

package message

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newOrderTestManager builds a manager with four areas in two conferences:
// A, C and D in conference 1, B in conference 2, positioned A B C D.
func newOrderTestManager(t *testing.T) (*MessageManager, string) {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMsgAreas(t, cfg, `[
		{"id":1,"position":1,"tag":"A","name":"A","conference_id":1},
		{"id":2,"position":2,"tag":"B","name":"B","conference_id":2},
		{"id":3,"position":3,"tag":"C","name":"C","conference_id":1},
		{"id":4,"position":4,"tag":"D","name":"D","conference_id":1}]`)
	mm, err := NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	return mm, cfg
}

// areaOrder returns the area tags in listing order, and fails if the positions
// are not the unbroken 1..n sequence a move is meant to leave behind.
func areaOrder(t *testing.T, mm *MessageManager) string {
	t.Helper()
	var tags []string
	for i, a := range mm.ListAreas() {
		if a.Position != i+1 {
			t.Errorf("area %s has position %d, want %d", a.Tag, a.Position, i+1)
		}
		tags = append(tags, a.Tag)
	}
	return strings.Join(tags, " ")
}

func TestMoveAreaPosition(t *testing.T) {
	for _, tc := range []struct {
		name        string
		areaID, pos int
		want        string
	}{
		{"last to first", 4, 1, "D A B C"},
		{"first to third", 1, 3, "B C A D"},
		{"same position", 2, 2, "A B C D"},
		{"below range clamps to first", 3, -5, "C A B D"},
		{"above range clamps to last", 1, 99, "B C D A"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mm, _ := newOrderTestManager(t)
			if err := mm.MoveAreaPosition(tc.areaID, tc.pos); err != nil {
				t.Fatalf("MoveAreaPosition: %v", err)
			}
			if got := areaOrder(t, mm); got != tc.want {
				t.Errorf("order = %q, want %q", got, tc.want)
			}
		})
	}

	mm, _ := newOrderTestManager(t)
	if err := mm.MoveAreaPosition(99, 1); !errors.Is(err, ErrAreaNotFound) {
		t.Errorf("moving a missing area: err = %v, want ErrAreaNotFound", err)
	}
}

// Moving within a conference must leave the other conference's area (B) in
// the slot it had.
func TestMoveAreaPositionInConference(t *testing.T) {
	for _, tc := range []struct {
		name          string
		areaID, index int
		want          string
	}{
		{"last to first in conference", 4, 1, "D B A C"},
		{"first to last in conference", 1, 3, "C B D A"},
		{"middle to first", 3, 1, "C B A D"},
		{"below range clamps to first", 4, 0, "D B A C"},
		{"above range clamps to last", 1, 99, "C B D A"},
		{"only area in its conference", 2, 1, "A B C D"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mm, _ := newOrderTestManager(t)
			if err := mm.MoveAreaPositionInConference(tc.areaID, tc.index); err != nil {
				t.Fatalf("MoveAreaPositionInConference: %v", err)
			}
			if got := areaOrder(t, mm); got != tc.want {
				t.Errorf("order = %q, want %q", got, tc.want)
			}
		})
	}

	mm, _ := newOrderTestManager(t)
	if err := mm.MoveAreaPositionInConference(99, 1); !errors.Is(err, ErrAreaNotFound) {
		t.Errorf("moving a missing area: err = %v, want ErrAreaNotFound", err)
	}
}

// A new order only matters once it is on disk: it has to survive a save and a
// fresh load.
func TestMovedOrderSurvivesSaveAndLoad(t *testing.T) {
	mm, cfg := newOrderTestManager(t)
	if err := mm.MoveAreaPosition(4, 1); err != nil {
		t.Fatalf("MoveAreaPosition: %v", err)
	}
	if err := mm.SaveAreas(); err != nil {
		t.Fatalf("SaveAreas: %v", err)
	}

	reloaded, err := NewMessageManager(filepath.Dir(cfg), cfg, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	if got := areaOrder(t, reloaded); got != "D A B C" {
		t.Errorf("order after reload = %q, want %q", got, "D A B C")
	}
}

// AddArea persists the new list; when that fails the area must not be left
// behind in memory where it would disagree with the file.
func TestAddAreaRollsBackWhenSaveFails(t *testing.T) {
	mm, cfg := newOrderTestManager(t)
	if err := os.RemoveAll(cfg); err != nil {
		t.Fatal(err)
	}

	id, err := mm.AddArea(MessageArea{Tag: "NEW", Name: "New", AreaType: "echomail", EchoTag: "NEW_ECHO", Network: "fsxnet"})
	if err == nil {
		t.Fatalf("AddArea succeeded with id %d although the config directory is gone", id)
	}
	if _, ok := mm.GetAreaByTag("NEW"); ok {
		t.Error("area NEW is still registered after the failed save")
	}
	if _, ok := mm.FindEchoAreaFold("NEW_ECHO", "fsxnet"); ok {
		t.Error("echo tag NEW_ECHO is still registered after the failed save")
	}
	if got := areaOrder(t, mm); got != "A B C D" {
		t.Errorf("order = %q, want the original %q", got, "A B C D")
	}
}

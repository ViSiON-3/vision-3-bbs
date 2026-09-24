package qwknet

import "testing"

func TestLoadRegistry(t *testing.T) {
	nets, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) == 0 || nets[0].Key != "dovenet" || nets[0].HubID != "VERT" || len(nets[0].Conferences) < 20 {
		t.Fatalf("registry = %+v", nets)
	}
}

package stringhistory

import (
	"testing"

	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// Every value the template ships now is in the history. If this fails,
// templates/configs/strings.json changed: run go generate
// ./internal/stringhistory and commit history.json with it.
func TestHistoryHasCurrentTemplate(t *testing.T) {
	shipped, err := configtemplates.StringDefaults()
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range shipped {
		if !Shipped(key, value) {
			t.Errorf("%s: current shipped value missing from history.json; run go generate ./internal/stringhistory", key)
		}
	}
}

func TestByHash(t *testing.T) {
	shipped, err := configtemplates.StringDefaults()
	if err != nil {
		t.Fatal(err)
	}
	v := shipped["pauseString"]
	if got, ok := ByHash("pauseString", Hash(v)); !ok || got != v {
		t.Errorf("ByHash = %q, %v; want %q", got, ok, v)
	}
	if _, ok := ByHash("pauseString", Hash("not a shipped value")); ok {
		t.Error("ByHash found a value that never shipped")
	}
}

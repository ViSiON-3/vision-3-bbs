package menu

import (
	"strings"
	"testing"
)

func TestGatePromptOrFallbackMissingFile(t *testing.T) {
	e := &MenuExecutor{MenuSetPath: t.TempDir()} // no ansi/ dir -> load fails
	got := gatePromptOrFallback(e, "DOES_NOT_EXIST.ASC", 1)
	if len(got) == 0 {
		t.Fatal("fallback prompt is empty")
	}
	if !strings.Contains(string(got), "##") {
		t.Errorf("fallback should contain a '##' countdown field, got %q", got)
	}
	if !strings.Contains(string(got), "{KEY}") || !strings.Contains(string(got), "{PRESSES}") {
		t.Errorf("fallback should contain unsubstituted {KEY}/{PRESSES} tokens, got %q", got)
	}
}

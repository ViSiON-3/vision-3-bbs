package menu

import (
	"strings"
	"testing"
)

func TestGatePromptOrFallbackMissingFile(t *testing.T) {
	e := &MenuExecutor{MenuSetPath: t.TempDir()} // no ansi/ dir -> load fails
	got := gatePromptOrFallback(e, "DOES_NOT_EXIST.ASC", 3, 1)
	if len(got) == 0 {
		t.Fatal("fallback prompt is empty")
	}
	if !strings.Contains(string(got), "##") {
		t.Errorf("fallback should contain a '##' countdown field, got %q", got)
	}
	if !strings.Contains(string(got), "3 time(s)") {
		t.Errorf("fallback should reflect the configured required press count, got %q", got)
	}
}

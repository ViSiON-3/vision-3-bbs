package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newIncludeExecutor(t *testing.T) *MenuExecutor {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "ansi"), 0755); err != nil {
		t.Fatalf("mkdir ansi: %v", err)
	}
	return &MenuExecutor{MenuSetPath: root}
}

func writeInclude(t *testing.T, e *MenuExecutor, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.MenuSetPath, "ansi", name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestProcessFileIncludes(t *testing.T) {
	e := newIncludeExecutor(t)
	writeInclude(t, e, "header.ans", "HEADER")
	writeInclude(t, e, "outer.ans", "[%%inner.ans%%]")
	writeInclude(t, e, "inner.ans", "NESTED")
	writeInclude(t, e, "loop.ans", "%%loop.ans%%")

	tests := []struct {
		name   string
		prompt string
		want   string
	}{
		{"no includes", "plain text", "plain text"},
		{"single include", "a %%header.ans%% b", "a HEADER b"},
		{"two includes", "%%header.ans%%+%%header.ans%%", "HEADER+HEADER"},
		{"nested include", "x %%outer.ans%% y", "x [NESTED] y"},
		{"missing file removed", "a %%nope.ans%% b", "a  b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.processFileIncludes(tt.prompt, 0)
			if err != nil {
				t.Fatalf("processFileIncludes: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("self-referential include stops at depth cap", func(t *testing.T) {
		got, err := e.processFileIncludes("%%loop.ans%%", 0)
		if err != nil {
			t.Fatalf("processFileIncludes: %v", err)
		}
		// Must terminate; the unresolved tag from the final depth remains.
		if !strings.Contains(got, "%%loop.ans%%") {
			t.Errorf("expected unresolved tag after depth cap, got %q", got)
		}
	})
}

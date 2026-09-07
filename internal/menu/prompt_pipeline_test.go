package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// promptExecutor builds an executor whose menu set contains the given ANSI
// files, so includes resolve during rendering.
func promptExecutor(t *testing.T, files map[string]string) *MenuExecutor {
	t.Helper()
	root := t.TempDir()
	ansiDir := filepath.Join(root, "ansi")
	if err := os.MkdirAll(ansiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(ansiDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return &MenuExecutor{MenuSetPath: root, RootConfigPath: t.TempDir()}
}

// visible strips ANSI escapes so assertions read against what appears on screen.
func visibleText(b []byte) string {
	return ansiEscapeRe.ReplaceAllString(string(b), "")
}

// The regression this guards: includes were resolved last, so nothing inside an
// included file was ever expanded, and the markup reached the screen verbatim.
func TestIncludedContentIsFullyExpanded(t *testing.T) {
	e := promptExecutor(t, map[string]string{
		"inc.ans": "handle=|UH level=|LEVEL users=@UC@|{ note=(|UN)|}",
	})
	placeholders := map[string]string{
		"|UH": "Felonius", "|LEVEL": "255", "|UN": "",
	}

	got := visibleText(e.renderPromptText("[%%inc.ans%%]", placeholders, 7, 2, 1))

	for _, leaked := range []string{"|UH", "|LEVEL", "@UC@", "|{", "|}"} {
		if strings.Contains(got, leaked) {
			t.Errorf("%q reached the output unexpanded: %q", leaked, got)
		}
	}
	for _, want := range []string{"Felonius", "255", "7"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in the rendered prompt, got %q", want, got)
		}
	}
	// The optional group held only an empty value, so it should be gone.
	if strings.Contains(got, "note=") {
		t.Errorf("optional group inside the include was not dropped: %q", got)
	}
}

// An optional group inside an include keeps its content when the value is set.
func TestIncludedOptionalGroupKeptWhenPopulated(t *testing.T) {
	e := promptExecutor(t, map[string]string{"inc.ans": "|{note=(|UN)|}"})

	got := visibleText(e.renderPromptText("%%inc.ans%%",
		map[string]string{"|UN": "SysOp"}, 0, 0, 1))

	if !strings.Contains(got, "note=(SysOp)") {
		t.Errorf("populated group inside an include was lost: %q", got)
	}
}

// Includes resolve before substitution, so a placeholder whose value happens to
// look like an include tag cannot pull in a file. |GL and |UN are user-settable,
// so this is the ordering that keeps that from being reachable.
func TestPlaceholderValueCannotTriggerAnInclude(t *testing.T) {
	e := promptExecutor(t, map[string]string{"secret.ans": "SHOULD-NOT-APPEAR"})

	got := visibleText(e.renderPromptText("loc=|GL",
		map[string]string{"|GL": "%%secret.ans%%"}, 0, 0, 1))

	if strings.Contains(got, "SHOULD-NOT-APPEAR") {
		t.Errorf("a placeholder value pulled in a file: %q", got)
	}
	if !strings.Contains(got, "%%secret.ans%%") {
		t.Errorf("the value should survive as literal text, got %q", got)
	}
}

// Ordering within substitution: a longer placeholder must not be eaten by a
// shorter one that prefixes it.
func TestLongerPlaceholdersSubstituteFirst(t *testing.T) {
	e := promptExecutor(t, nil)

	got := visibleText(e.renderPromptText("|CAN/|CA",
		map[string]string{"|CA": "SHORT", "|CAN": "LONG"}, 0, 0, 1))

	if got != "LONG/SHORT" {
		t.Errorf("got %q, want %q", got, "LONG/SHORT")
	}
}

// A missing include is skipped rather than failing the prompt, which is why
// the pipeline has no error to return.
func TestMissingIncludeLeavesTheRestIntact(t *testing.T) {
	e := promptExecutor(t, nil)

	got := visibleText(e.renderPromptText("a %%nope.ans%% |UH",
		map[string]string{"|UH": "Felonius"}, 0, 0, 1))

	if !strings.Contains(got, "Felonius") {
		t.Errorf("a missing include broke the rest of the prompt: %q", got)
	}
}

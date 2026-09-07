package menu

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// The include cap has to stop a cycle rather than recursing until the stack
// gives out. A file that includes itself is the shape that matters.
func TestSelfIncludingFileTerminates(t *testing.T) {
	e := promptExecutor(t, map[string]string{
		"loop.ans": "x%%loop.ans%%",
	})

	done := make(chan string, 1)
	go func() { done <- visibleText(e.renderPromptText("%%loop.ans%%", nil, 0, 0, 1)) }()

	select {
	case got := <-done:
		// Each round appends one "x", so the count is the number of rounds that
		// ran. Asserting the configured bound exactly, rather than some larger
		// ceiling, means a regression that expands seven times is caught.
		if rounds := strings.Count(got, "x"); rounds != maxIncludeRounds {
			t.Errorf("expanded %d rounds, want exactly %d: %q", rounds, maxIncludeRounds, got)
		}
		// The unexpanded tag is left in place rather than silently dropped, so
		// a menu set that hits the cap shows evidence of it.
		if !strings.Contains(got, "%%loop.ans%%") {
			t.Errorf("the tag that hit the cap was dropped rather than left visible: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a self-including file did not terminate")
	}
}

// A legitimate chain nested right up to the cap must expand fully and quietly.
// Recursing because a round did work, rather than because work remains, made
// the last expansion recurse once more purely to trip the limit -- so a valid
// menu set logged "exceeded maximum" and looked broken.
func TestNestingExactlyAtTheCapExpandsWithoutWarning(t *testing.T) {
	files := map[string]string{}
	for i := 1; i < maxIncludeRounds; i++ {
		files[fmt.Sprintf("n%d.ans", i)] = fmt.Sprintf("%%%%n%d.ans%%%%", i+1)
	}
	files[fmt.Sprintf("n%d.ans", maxIncludeRounds)] = "BOTTOM"
	e := promptExecutor(t, files)

	var logged bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(restore)

	got := visibleText(e.renderPromptText("%%n1.ans%%", nil, 0, 0, 1))

	if !strings.Contains(got, "BOTTOM") {
		t.Errorf("a chain nested to the cap did not expand fully: %q", got)
	}
	if strings.Contains(got, "%%") {
		t.Errorf("an include tag survived: %q", got)
	}
	if strings.Contains(logged.String(), "exceeded maximum") {
		t.Errorf("a valid nest warned about the cap: %s", logged.String())
	}
}

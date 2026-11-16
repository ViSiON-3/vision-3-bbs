package usereditor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// The Custom Prompt field takes the 80 characters a caller can keep, though
// it shows 22, and stores the prompt cleaned as Konfig does.
func TestCustomPromptFieldTakesEighty(t *testing.T) {
	m, _ := editBob(t)
	m = focusField(t, m, "Custom Prompt")
	m = replaceField(t, m, strings.Repeat("x", 90))
	if got := len([]rune(m.textInput.Value())); got != user.CustomPromptMaxLen {
		t.Fatalf("field took %d characters, want %d", got, user.CustomPromptMaxLen)
	}
	m = press(t, m, key(tea.KeyEnter))
	if got := m.users[m.editIndex].CustomPrompt; got != strings.Repeat("x", user.CustomPromptMaxLen) {
		t.Fatalf("CustomPrompt = %q", got)
	}

	m = focusField(t, m, "Custom Prompt")
	m = replaceField(t, m, "  |11Mine>  ")
	m = press(t, m, key(tea.KeyEnter))
	if got := m.users[m.editIndex].CustomPrompt; got != "|11Mine>" {
		t.Fatalf("CustomPrompt = %q, want it trimmed", got)
	}
}

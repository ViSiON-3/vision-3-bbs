package menu

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// TestShippedLoginSequencesAreDispatchable guards the gap that let SYSOPNOTICES
// ship in the default login sequence — and in the login.json every new board is
// installed with — while the runner had no handler for it, so the step logged
// "unknown login sequence command" and was skipped on every login.
//
// Registering a command in MenuExecutor.RunRegistry is not enough: the login
// sequence dispatches through loginSequenceHandlers.
func TestShippedLoginSequencesAreDispatchable(t *testing.T) {
	assertDispatchable := func(t *testing.T, source string, items []config.LoginItem) {
		t.Helper()
		if len(items) == 0 {
			t.Fatalf("%s: expected a non-empty login sequence", source)
		}
		for i, item := range items {
			cmd := strings.ToUpper(strings.TrimSpace(item.Command))
			if cmd == "" {
				t.Errorf("%s: item %d has an empty command", source, i+1)
				continue
			}
			// DOOR: items are dispatched through RunRegistry["DOOR:"] instead.
			if strings.HasPrefix(cmd, "DOOR:") {
				continue
			}
			if _, ok := loginSequenceHandlers[cmd]; !ok {
				t.Errorf("%s: item %d command %q has no entry in loginSequenceHandlers, so the login sequence would skip it",
					source, i+1, cmd)
			}
		}
	}

	t.Run("shipped template", func(t *testing.T) {
		data, err := configtemplates.FS.ReadFile("login.json")
		if err != nil {
			t.Fatalf("reading embedded templates/configs/login.json: %v", err)
		}
		var items []config.LoginItem
		if err := json.Unmarshal(data, &items); err != nil {
			t.Fatalf("parsing templates/configs/login.json: %v", err)
		}
		assertDispatchable(t, "templates/configs/login.json", items)
	})

	t.Run("built-in default", func(t *testing.T) {
		// A missing login.json falls back to config.defaultLoginSequence().
		items, err := config.LoadLoginSequence(t.TempDir())
		if err != nil {
			t.Fatalf("LoadLoginSequence with no file: %v", err)
		}
		assertDispatchable(t, "config.defaultLoginSequence()", items)
	})
}

// TestShippedLoginSequencesLeadWithPriorityItems pins the ordering rule that
// SYSOPNOTICES and NMAILSCAN come before FASTLOGIN. A FASTLOGIN jump returns a
// GOTO, which ends the sequence outright (see runFullLoginSequence), so any
// item placed after it is skipped whenever a caller takes the shortcut. These
// two carry news nothing else repeats — queued sysop notices are cleared once
// delivered, and unread mail is otherwise only discoverable by hand — so they
// must not sit behind an early exit.
func TestShippedLoginSequencesLeadWithPriorityItems(t *testing.T) {
	priority := []string{"SYSOPNOTICES", "NMAILSCAN"}

	assertPriorityLeads := func(t *testing.T, source string, items []config.LoginItem) {
		t.Helper()
		commands := make([]string, len(items))
		for i, item := range items {
			commands[i] = strings.ToUpper(strings.TrimSpace(item.Command))
		}
		indexOf := func(cmd string) int {
			for i, c := range commands {
				if c == cmd {
					return i
				}
			}
			return -1
		}

		exit := indexOf("FASTLOGIN")
		for _, cmd := range priority {
			at := indexOf(cmd)
			if at < 0 {
				t.Errorf("%s: expected %s in the sequence", source, cmd)
				continue
			}
			if exit >= 0 && at > exit {
				t.Errorf("%s: %s is at position %d, after FASTLOGIN at %d — a fast-login jump would skip it",
					source, cmd, at+1, exit+1)
			}
		}
	}

	t.Run("shipped template", func(t *testing.T) {
		data, err := configtemplates.FS.ReadFile("login.json")
		if err != nil {
			t.Fatalf("reading embedded templates/configs/login.json: %v", err)
		}
		var items []config.LoginItem
		if err := json.Unmarshal(data, &items); err != nil {
			t.Fatalf("parsing templates/configs/login.json: %v", err)
		}
		assertPriorityLeads(t, "templates/configs/login.json", items)
	})

	t.Run("built-in default", func(t *testing.T) {
		items, err := config.LoadLoginSequence(t.TempDir())
		if err != nil {
			t.Fatalf("LoadLoginSequence with no file: %v", err)
		}
		assertPriorityLeads(t, "config.defaultLoginSequence()", items)
	})
}

package menu

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// menuCommandsDocPath is the centralized menu command reference. Every RUN:
// target registered in the executor, and every login.json step name, must have
// a row there, so a new command cannot ship without a sysop-facing description.
const menuCommandsDocPath = "../../docs/sysop/reference/menu-commands.md"

// docCommandRowRE matches a reference table row whose first cell is the
// backticked command name, e.g. "| `LASTCALLERS` | ...".
var docCommandRowRE = regexp.MustCompile("(?m)^\\| *`([A-Z0-9_:]+)`")

func documentedMenuCommands(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(menuCommandsDocPath))
	if err != nil {
		t.Fatalf("read %s: %v", menuCommandsDocPath, err)
	}
	documented := make(map[string]bool)
	for _, m := range docCommandRowRE.FindAllStringSubmatch(string(data), -1) {
		documented[m[1]] = true
	}
	return documented
}

func TestEveryRegisteredMenuCommandIsDocumented(t *testing.T) {
	registry := make(map[string]RunnableFunc)
	registerPlaceholderRunnables(registry)
	registerAppRunnables(registry)

	documented := documentedMenuCommands(t)

	var missing []string
	for name := range registry {
		if !documented[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("registered menu commands with no row in %s:\n  %s",
			menuCommandsDocPath, strings.Join(missing, "\n  "))
	}
}

func TestEveryLoginSequenceStepIsDocumented(t *testing.T) {
	documented := documentedMenuCommands(t)

	var missing []string
	for name := range loginSequenceHandlers {
		if !documented[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("login.json step names with no row in %s:\n  %s",
			menuCommandsDocPath, strings.Join(missing, "\n  "))
	}
}

func TestEveryDocumentedMenuCommandIsRegistered(t *testing.T) {
	registry := make(map[string]RunnableFunc)
	registerPlaceholderRunnables(registry)
	registerAppRunnables(registry)

	var stale []string
	for name := range documentedMenuCommands(t) {
		_, isRun := registry[name]
		_, isLogin := loginSequenceHandlers[name]
		if !isRun && !isLogin {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("names documented in %s that are neither RUN: targets nor login.json steps (typo or removed command):\n  %s",
			menuCommandsDocPath, strings.Join(stale, "\n  "))
	}
}

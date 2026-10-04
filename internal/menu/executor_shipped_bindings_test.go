package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestShippedMenuBindingsDispatch exercises every user-selectable key in the
// shipped menu configs through the same matcher used by the executor. Handler
// tests cover command behavior; this catches a key/config/matcher regression
// even when no live terminal journey uses that menu.
func TestShippedMenuBindingsDispatch(t *testing.T) {
	menuRoot := filepath.Join("..", "..", "menus", "v3")
	entries, err := os.ReadDir(filepath.Join(menuRoot, "cfg"))
	if err != nil {
		t.Fatalf("read shipped menu configs: %v", err)
	}
	menus := menuset.Bare(menuRoot)
	allowAll := func(string, string) bool { return true }
	checked := 0

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".CFG" {
			continue
		}
		menuName := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		commands, err := LoadCommands(menuName, menus)
		if err != nil {
			t.Fatalf("load %s: %v", entry.Name(), err)
		}
		for index, command := range commands {
			for _, key := range strings.Fields(command.Keys) {
				if key == "//" || key == "~~" {
					// These keys mark auto-run commands; Run dispatches them before
					// displaying the menu and never sends them through matchCommand.
					continue
				}
				input := key
				want := command.Command
				switch key {
				case "^M":
					input = ""
				case "##":
					input = "42"
					want += " " + input
				}

				got, _, matched := matchCommand(commands, input, allowAll)
				if !matched || got != want {
					t.Errorf("%s.CFG action %d key %q dispatched to (%q, %v), want %q", menuName, index, key, got, matched, want)
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("no shipped menu bindings were checked")
	}
	t.Logf("checked %d shipped menu key bindings", checked)
}

// TestShippedMenuActionsReachRegisteredHandlers follows shipped interactive
// bindings through command execution for action types with a direct executor
// route. Fake handlers make the check side-effect free while proving that
// each configured RUN target and DOOR name reaches the expected registry.
func TestShippedMenuActionsReachRegisteredHandlers(t *testing.T) {
	env := newMenuEnv(t)
	menuRoot := filepath.Join("..", "..", "menus", "v3")
	entries, err := os.ReadDir(filepath.Join(menuRoot, "cfg"))
	if err != nil {
		t.Fatalf("read shipped menu configs: %v", err)
	}
	menus := menuset.Bare(menuRoot)
	allowAll := func(string, string) bool { return true }
	type call struct{ target, args string }
	var calls []call
	for target, handler := range env.e.RunRegistry {
		if target == "DOOR:" {
			continue
		}
		if handler == nil {
			t.Fatalf("nil registered handler for %s", target)
		}
		target := target
		env.e.RunRegistry[target] = func(c *cmdCtx, args string) (*user.User, string, error) {
			calls = append(calls, call{target: target, args: args})
			return c.currentUser, "", nil
		}
	}
	var doors []string
	if env.e.RunRegistry["DOOR:"] == nil {
		t.Fatal("DOOR handler is not registered")
	}
	env.e.RunRegistry["DOOR:"] = func(c *cmdCtx, name string) (*user.User, string, error) {
		doors = append(doors, name)
		return c.currentUser, "", nil
	}

	session := newTestSession("")
	terminal := newTestTerminal(session)
	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".CFG" {
			continue
		}
		menuName := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		commands, err := LoadCommands(menuName, menus)
		if err != nil {
			t.Fatalf("load %s: %v", entry.Name(), err)
		}
		for _, command := range commands {
			for _, key := range strings.Fields(command.Keys) {
				if key == "//" || key == "~~" {
					continue
				}
				input := key
				switch key {
				case "^M":
					input = ""
				case "##":
					input = "42"
				}
				action, _, matched := matchCommand(commands, input, allowAll)
				if !matched {
					continue
				}
				if !strings.HasPrefix(action, "GOTO:") && !strings.HasPrefix(action, "RUN:") && !strings.HasPrefix(action, "DOOR:") && action != "LOGOFF" {
					continue // PDMATRIX and sponsor-menu commands use dedicated dispatch paths.
				}
				beforeCalls, beforeDoors := len(calls), len(doors)
				actionType, nextMenu, resultUser := env.e.executeCommandAction(action, session, terminal, env.um, env.caller, 1, time.Now(), env.outputMode, 80, 24)
				if resultUser != env.caller {
					t.Errorf("%s key %q returned user %v, want caller", menuName, key, resultUser)
				}
				switch {
				case strings.HasPrefix(action, "GOTO:"):
					want := strings.ToUpper(strings.TrimPrefix(action, "GOTO:"))
					if actionType != "GOTO" || nextMenu != want {
						t.Errorf("%s key %q: route (%q, %q), want (GOTO, %q)", menuName, key, actionType, nextMenu, want)
					}
				case strings.HasPrefix(action, "RUN:"):
					parts := strings.SplitN(strings.TrimPrefix(action, "RUN:"), " ", 2)
					wantArgs := ""
					if len(parts) == 2 {
						wantArgs = parts[1]
					}
					if actionType != "CONTINUE" || len(calls) != beforeCalls+1 || calls[beforeCalls] != (call{target: strings.ToUpper(parts[0]), args: wantArgs}) {
						t.Errorf("%s key %q: RUN route calls=%v action=%q, want target %s args %q", menuName, key, calls[beforeCalls:], actionType, strings.ToUpper(parts[0]), wantArgs)
					}
				case strings.HasPrefix(action, "DOOR:"):
					want := strings.TrimPrefix(action, "DOOR:")
					if actionType != "CONTINUE" || len(doors) != beforeDoors+1 || doors[beforeDoors] != want {
						t.Errorf("%s key %q: DOOR route doors=%v action=%q, want %q", menuName, key, doors[beforeDoors:], actionType, want)
					}
				case action == "LOGOFF":
					if actionType != "LOGOFF" {
						t.Errorf("%s key %q: action type %q, want LOGOFF", menuName, key, actionType)
					}
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("no shipped executor routes were checked")
	}
	t.Logf("checked %d shipped key-to-executor routes", checked)
}

package bbsregression

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

type menuActionCoverage struct {
	Menu           string   `json:"menu"`
	Index          int      `json:"index"`
	Keys           string   `json:"keys"`
	Command        string   `json:"command"`
	Coverage       string   `json:"coverage"`
	TerminalTest   string   `json:"terminalTest"`
	TerminalOutput string   `json:"terminalOutput,omitempty"`
	HandlerTests   []string `json:"handlerTests"`
}

// TestShippedMenuActionsHaveCoverageInventory keeps the action audit in sync
// with the shipped .CFG files. Every action must link to a terminal journey or
// command-handler test, so config changes cannot silently evade the test
// inventory. This checks links and selected route/output evidence; it does not
// prove each linked test asserts the action's full behavior.
func TestShippedMenuActionsHaveCoverageInventory(t *testing.T) {
	root := filepath.Join("..", "..", "menus", "v3")
	coveragePath := filepath.Join("..", "..", "docs", "development", "menu-action-coverage.json")
	data, err := os.ReadFile(coveragePath)
	if err != nil {
		t.Fatalf("read menu action inventory: %v", err)
	}
	var inventory []menuActionCoverage
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatalf("decode menu action inventory: %v", err)
	}

	byAction := make(map[string]menuActionCoverage, len(inventory))
	journeys := loadTerminalJourneys(t)
	handlerTests := loadMenuHandlerTestEvidence(t)
	runnableHandlers := loadRunnableHandlers(t)
	for _, action := range inventory {
		id := menuActionID(action.Menu, action.Index, action.Keys)
		if _, exists := byAction[id]; exists {
			t.Errorf("duplicate inventory entry for %s", id)
		}
		if action.Command == "" {
			t.Errorf("%s has no command", id)
		}
		if action.TerminalTest != "" {
			journey, exists := journeys[action.TerminalTest]
			if !exists {
				t.Errorf("%s references missing terminal subtest %q", id, action.TerminalTest)
			} else if !journey.usesMenuKeys(action.Keys) {
				t.Errorf("%s terminal subtest %q does not send one of the configured keys", id, action.TerminalTest)
			} else if target, isGoto := menuGotoTarget(action.Command); isGoto && target != "MAIN" {
				labels := menuVisibleLabels(t, root, target)
				if len(labels) > 0 && !journey.assertsAnyLabel(labels) {
					t.Errorf("%s terminal subtest %q does not assert a destination label for %s: %v", id, action.TerminalTest, target, labels)
				}
			} else if target, isDoor := menuDoorTarget(action.Command); isDoor {
				label := menuDoorVisibleLabel(target)
				if label == "" {
					t.Errorf("%s has no expected visible output for DOOR:%s", id, target)
				} else if !journey.assertsText(label) {
					t.Errorf("%s terminal subtest %q does not assert door output %q", id, action.TerminalTest, label)
				}
			}
		}
		if action.TerminalOutput != "" {
			if action.TerminalTest == "" {
				t.Errorf("%s declares terminal output without a terminal test", id)
			} else if journey, exists := journeys[action.TerminalTest]; exists && !journey.assertsText(action.TerminalOutput) {
				t.Errorf("%s terminal subtest %q does not assert expected output %q", id, action.TerminalTest, action.TerminalOutput)
			}
		}
		coveredRunnable := false
		runTarget, hasRunTarget := menuRunTarget(action.Command)
		for _, testName := range action.HandlerTests {
			evidence, exists := handlerTests[testName]
			if !exists {
				t.Errorf("%s references missing internal/menu test %q", id, testName)
				continue
			}
			if hasRunTarget && evidence.testsRunnable(runTarget, runnableHandlers[runTarget]) {
				coveredRunnable = true
			}
		}
		if hasRunTarget && !coveredRunnable {
			t.Errorf("%s has no linked test that exercises RUN:%s or its registered handler", id, runTarget)
		}
		wantCoverage := "uncovered"
		if action.TerminalTest != "" {
			wantCoverage = "terminal"
		}
		if len(action.HandlerTests) > 0 {
			if wantCoverage == "terminal" {
				wantCoverage = "terminal+handler"
			} else {
				wantCoverage = "handler"
			}
		}
		if action.Coverage != wantCoverage {
			t.Errorf("%s coverage = %q, want %q from its test references", id, action.Coverage, wantCoverage)
		}
		if wantCoverage == "uncovered" {
			t.Errorf("%s (%s) has no linked test reference", id, action.Command)
		}
		byAction[id] = action
	}

	entries, err := os.ReadDir(filepath.Join(root, "cfg"))
	if err != nil {
		t.Fatalf("read shipped menu configs: %v", err)
	}
	menus := menuset.Bare(root)
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".CFG" {
			continue
		}
		name := entry.Name()[:len(entry.Name())-len(filepath.Ext(entry.Name()))]
		commands, err := menu.LoadCommands(name, menus)
		if err != nil {
			t.Fatalf("load %s: %v", entry.Name(), err)
		}
		for i, command := range commands {
			id := menuActionID(name, i, command.Keys)
			seen[id] = true
			action, ok := byAction[id]
			if !ok {
				t.Errorf("%s command %q is missing from %s", id, command.Command, coveragePath)
				continue
			}
			if action.Command != command.Command {
				t.Errorf("%s inventory command = %q, shipped command = %q", id, action.Command, command.Command)
			}
			if action.Keys != command.Keys {
				t.Errorf("%s inventory keys = %q, shipped keys = %q", id, action.Keys, command.Keys)
			}
		}
	}

	var stale []string
	for id := range byAction {
		if !seen[id] {
			stale = append(stale, id)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("inventory entries no longer found in shipped configs: %v", stale)
	}

	uncovered := 0
	for _, action := range byAction {
		if action.Coverage == "uncovered" {
			uncovered++
		}
	}
	withTerminal := 0
	withHandler := 0
	for _, action := range byAction {
		if action.TerminalTest != "" {
			withTerminal++
		}
		if len(action.HandlerTests) > 0 {
			withHandler++
		}
	}
	t.Logf("shipped menu action inventory: %d total, %d with terminal journey links, %d with handler test links, %d without test links", len(byAction), withTerminal, withHandler, uncovered)
	if uncovered > 0 {
		t.Logf("action gaps are listed in %s", coveragePath)
	}
}

type terminalJourneyEvidence struct {
	keys     map[string]bool
	patterns map[string]bool
}

func (e terminalJourneyEvidence) usesMenuKeys(configKeys string) bool {
	for _, key := range strings.Fields(strings.ToUpper(configKeys)) {
		if e.keys[key] {
			return true
		}
	}
	return false
}

func (e terminalJourneyEvidence) assertsAnyLabel(labels []string) bool {
	for pattern := range e.patterns {
		for _, label := range labels {
			if strings.Contains(strings.ToLower(pattern), strings.ToLower(label)) {
				return true
			}
		}
	}
	return false
}

func (e terminalJourneyEvidence) assertsText(want string) bool {
	for pattern := range e.patterns {
		if strings.Contains(strings.ToLower(pattern), strings.ToLower(want)) {
			return true
		}
	}
	return false
}

func loadTerminalJourneys(t *testing.T) map[string]terminalJourneyEvidence {
	t.Helper()
	path := "regression_live_test.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse terminal regression journeys: %v", err)
	}
	journeys := make(map[string]terminalJourneyEvidence)
	ast.Inspect(file, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(fn.Name.Name, "TestLocal") {
			return true
		}
		// Table-driven journeys keep their expected key and screen in literal
		// records, so extract those records alongside explicit t.Run calls.
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			record, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			fields := make(map[string]string)
			for _, element := range record.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := field.Key.(*ast.Ident)
				value, valueOK := field.Value.(*ast.BasicLit)
				if !ok || !valueOK || value.Kind != token.STRING {
					continue
				}
				textValue, err := strconv.Unquote(value.Value)
				if err == nil {
					fields[key.Name] = textValue
				}
			}
			if name, hasName := fields["name"]; hasName {
				keys := fields["keys"]
				if keys == "" {
					keys = fields["key"]
				}
				if keys != "" {
					pattern := fields["want"]
					if pattern == "" {
						pattern = fields["menu"]
					}
					patterns := make(map[string]bool)
					if pattern != "" {
						patterns[pattern] = true
					}
					journeys[fn.Name.Name+"/"+name] = terminalJourneyEvidence{keys: menuInputKeys(keys), patterns: patterns}
				}
			}
			return true
		})
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Run" {
				return true
			}
			nameLiteral, ok := call.Args[0].(*ast.BasicLit)
			if !ok || nameLiteral.Kind != token.STRING {
				return true
			}
			callback, ok := call.Args[1].(*ast.FuncLit)
			if !ok {
				return true
			}
			var subtest string
			if err := json.Unmarshal([]byte(nameLiteral.Value), &subtest); err == nil {
				journey := terminalJourneyEvidence{keys: make(map[string]bool), patterns: make(map[string]bool)}
				ast.Inspect(callback.Body, func(node ast.Node) bool {
					send, ok := node.(*ast.CallExpr)
					if !ok || len(send.Args) < 2 {
						return true
					}
					method, ok := send.Fun.(*ast.SelectorExpr)
					if !ok || (method.Sel.Name != "send" && method.Sel.Name != "sendExpect") {
						return true
					}
					input, ok := send.Args[1].(*ast.BasicLit)
					if ok && input.Kind == token.STRING {
						var value string
						if err := json.Unmarshal([]byte(input.Value), &value); err == nil {
							for key := range menuInputKeys(value) {
								journey.keys[key] = true
							}
						}
					}
					if method.Sel.Name == "sendExpect" && len(send.Args) > 2 {
						pattern, ok := send.Args[2].(*ast.BasicLit)
						if ok && pattern.Kind == token.STRING {
							value, err := strconv.Unquote(pattern.Value)
							if err == nil {
								journey.patterns[value] = true
							}
						}
					}
					return true
				})
				journeys["TestLocalTerminalRegressionSuite/"+subtest] = journey
			}
			return true
		})
		return false
	})
	return journeys
}

func menuGotoTarget(command string) (string, bool) {
	if !strings.HasPrefix(command, "GOTO:") {
		return "", false
	}
	target := strings.TrimSpace(strings.TrimPrefix(command, "GOTO:"))
	if target == "" {
		return "", false
	}
	return strings.ToUpper(target), true
}

func menuDoorTarget(command string) (string, bool) {
	if !strings.HasPrefix(strings.ToUpper(command), "DOOR:") {
		return "", false
	}
	target := strings.TrimSpace(command[len("DOOR:"):])
	return strings.ToUpper(target), target != ""
}

func menuDoorVisibleLabel(target string) string {
	return map[string]string{
		"HELLO":     "Thanks for trying VPL scripting",
		"ONELINERS": "Add an oneliner",
		"VOTING":    "Your choice",
		"STATS":     "System Statistics",
		"AUTOMSG":   "Leave a new auto-message",
		"USERSTATS": "Top Callers",
		"LAST10":    "Last 10 Callers",
	}[target]
}

func menuVisibleLabels(t *testing.T, root, menuName string) []string {
	t.Helper()
	path := filepath.Join(root, "mnu", menuName+".MNU")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read destination menu %s: %v", menuName, err)
	}
	var record struct {
		Title   string `json:"TITLE"`
		Prompt1 string `json:"PROMPT1"`
		Prompt2 string `json:"PROMPT2"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode destination menu %s: %v", menuName, err)
	}
	labels := []string{record.Title}
	if words := strings.Fields(record.Title); len(words) > 0 {
		labels = append(labels, words[0])
	}
	pipeToken := regexp.MustCompile(`\|(?:[0-9A-Za-z]{2,}|[0-9]{2})`)
	bracketed := regexp.MustCompile(`\[([^\]]+)\]`)
	for _, prompt := range []string{record.Prompt1, record.Prompt2} {
		plain := pipeToken.ReplaceAllString(prompt, "")
		for _, match := range bracketed.FindAllStringSubmatch(plain, -1) {
			if text := strings.TrimSpace(match[1]); text != "" {
				labels = append(labels, text)
			}
		}
		if !strings.Contains(plain, "Command") {
			if text := strings.TrimSpace(plain); text != "" {
				labels = append(labels, text)
			}
		}
	}
	return labels
}

func menuInputKeys(input string) map[string]bool {
	keys := make(map[string]bool)
	for _, field := range strings.Fields(strings.ToUpper(input)) {
		if strings.HasPrefix(field, "{{") {
			keys["{"] = true
			continue
		}
		key := strings.SplitN(field, "{", 2)[0]
		key = strings.TrimSpace(key)
		if key != "" {
			keys[key] = true
		}
	}
	return keys
}

type menuHandlerTestEvidence struct {
	commandLiterals map[string]bool
	runCmdTargets   map[string]bool
	identifiers     map[string]bool
	runCmdCalls     bool
}

func (e menuHandlerTestEvidence) testsRunnable(target, handler string) bool {
	if e.runCmdTargets[target] || (e.commandLiterals[target] && e.runCmdCalls) {
		return true
	}
	if handler != "" && e.identifiers[handler] {
		return true
	}
	// FASTLOGN's "1" entry dispatches FULL_LOGIN_SEQUENCE through the
	// FASTLOGIN lightbar, rather than calling that registered runnable directly.
	return target == "FULL_LOGIN_SEQUENCE" && e.runCmdTargets["FASTLOGIN"] && e.commandLiterals["1"]
}

func menuRunTarget(command string) (string, bool) {
	if !strings.HasPrefix(command, "RUN:") {
		return "", false
	}
	fields := strings.Fields(strings.TrimPrefix(command, "RUN:"))
	if len(fields) == 0 {
		return "", false
	}
	return strings.ToUpper(fields[0]), true
}

func loadMenuHandlerTestEvidence(t *testing.T) map[string]menuHandlerTestEvidence {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("..", "menu"))
	if err != nil {
		t.Fatalf("read menu test files: %v", err)
	}
	tests := make(map[string]menuHandlerTestEvidence)
	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join("..", "menu", entry.Name())
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse menu tests %s: %v", entry.Name(), err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && len(fn.Name.Name) >= 4 && fn.Name.Name[:4] == "Test" {
				evidence := menuHandlerTestEvidence{
					commandLiterals: make(map[string]bool),
					runCmdTargets:   make(map[string]bool),
					identifiers:     make(map[string]bool),
				}
				ast.Inspect(fn.Body, func(node ast.Node) bool {
					switch n := node.(type) {
					case *ast.Ident:
						evidence.identifiers[n.Name] = true
					case *ast.BasicLit:
						if n.Kind == token.STRING {
							var value string
							if err := json.Unmarshal([]byte(n.Value), &value); err == nil {
								evidence.commandLiterals[strings.ToUpper(value)] = true
							}
						}
					case *ast.CallExpr:
						if selector, ok := n.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "runCmd" {
							evidence.runCmdCalls = true
							if len(n.Args) > 0 {
								if arg, ok := n.Args[0].(*ast.BasicLit); ok && arg.Kind == token.STRING {
									if value, err := strconv.Unquote(arg.Value); err == nil {
										evidence.runCmdTargets[strings.ToUpper(value)] = true
									}
								}
							}
						}
					}
					return true
				})
				tests[fn.Name.Name] = evidence
			}
		}
	}
	return tests
}

func loadRunnableHandlers(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join("..", "menu", "executor_runnables_registry.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse runnable registry: %v", err)
	}
	handlers := make(map[string]string)
	ast.Inspect(file, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			return true
		}
		index, ok := assignment.Lhs[0].(*ast.IndexExpr)
		if !ok {
			return true
		}
		key, ok := index.Index.(*ast.BasicLit)
		if !ok || key.Kind != token.STRING {
			return true
		}
		var name string
		if err := json.Unmarshal([]byte(key.Value), &name); err != nil {
			return true
		}
		handler, ok := assignment.Rhs[0].(*ast.Ident)
		if ok {
			handlers[strings.ToUpper(name)] = handler.Name
		}
		return true
	})
	return handlers
}

func menuActionID(menuName string, index int, keys string) string {
	return fmt.Sprintf("%s[%d]:%q", menuName, index, keys)
}

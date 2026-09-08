package stringformat_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/formatspec"
	"github.com/ViSiON-3/vision-3-bbs/internal/stringformat"
)

// callSite is one place the source hands a configured string to fmt.
type callSite struct {
	Key      string // strings.json key
	Field    string // StringsConfig field name
	Pos      string // file:line
	Args     int    // variadic arguments passed after the format string
	Variadic bool   // the call spreads a slice, so the count is unknown
}

// formatFuncs are the fmt entry points whose first argument is a format string.
// Fprintf is included with its format at index 1.
var formatFuncs = map[string]int{
	"Sprintf": 0,
	"Printf":  0,
	"Errorf":  0,
	"Fprintf": 1,
}

// fieldToKey maps a StringsConfig field name to its strings.json key.
func fieldToKey(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	rt := reflect.TypeOf(config.StringsConfig{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if key, _, _ := strings.Cut(f.Tag.Get("json"), ","); key != "" {
			out[f.Name] = key
		}
	}
	return out
}

// stringsFieldName returns the StringsConfig field an expression selects, if
// the expression is of the form <anything>.LoadedStrings.<Field>.
func stringsFieldName(expr ast.Expr) (string, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	inner, ok := sel.X.(*ast.SelectorExpr)
	if !ok || inner.Sel.Name != "LoadedStrings" {
		return "", false
	}
	return sel.Sel.Name, true
}

// scanCallSites walks the repository for calls that pass a configured string to
// a fmt function.
func scanCallSites(t *testing.T) []callSite {
	t.Helper()
	keys := fieldToKey(t)
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()

	var sites []callSite
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "third_party", "node_modules", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil // not our concern here; the build catches syntax errors
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := fn.X.(*ast.Ident)
			if !ok || pkg.Name != "fmt" {
				return true
			}
			at, ok := formatFuncs[fn.Sel.Name]
			if !ok || len(call.Args) <= at {
				return true
			}
			field, ok := stringsFieldName(call.Args[at])
			if !ok {
				return true
			}
			key, ok := keys[field]
			if !ok {
				t.Errorf("%s: LoadedStrings.%s has no json tag", fset.Position(call.Pos()), field)
				return true
			}
			sites = append(sites, callSite{
				Key:      key,
				Field:    field,
				Pos:      fset.Position(call.Pos()).String(),
				Args:     len(call.Args) - at - 1,
				Variadic: call.Ellipsis.IsValid(),
			})
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking source: %v", err)
	}
	return sites
}

// shippedDefaults loads the factory template.
func shippedDefaults(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join("..", "..", "templates", "configs", "strings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return values
}

// TestFormattedKeysMatchCallSites is the coverage guard: the committed list
// must be exactly the set of keys the source hands to fmt. A newly formatted
// string therefore cannot be added without also being validated.
func TestFormattedKeysMatchCallSites(t *testing.T) {
	found := map[string]bool{}
	for _, s := range scanCallSites(t) {
		found[s.Key] = true
	}
	committed := map[string]bool{}
	for _, k := range stringformat.FormattedKeys {
		committed[k] = true
	}

	var missing, extra []string
	for k := range found {
		if !committed[k] {
			missing = append(missing, k)
		}
	}
	for k := range committed {
		if !found[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("these keys are passed to fmt but are not in FormattedKeys, so "+
			"their arguments are unchecked; add them to keys.go:\n  %s",
			strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("these keys are in FormattedKeys but no longer reach fmt; "+
			"remove them from keys.go:\n  %s", strings.Join(extra, "\n  "))
	}
}

// TestCallSiteArgsMatchShippedDefaults ties the shipped defaults to the
// arguments the call sites actually pass. Comparing two copies of a default
// proves nothing about the call site; this compares the default against the
// code that fills it.
func TestCallSiteArgsMatchShippedDefaults(t *testing.T) {
	defaults := shippedDefaults(t)
	var skipped []string

	for _, site := range scanCallSites(t) {
		if site.Variadic {
			// The call spreads a slice, so the count is not known statically.
			skipped = append(skipped, site.Key+" at "+site.Pos)
			continue
		}
		def, ok := defaults[site.Key]
		if !ok {
			// Covered separately: every runtime string should ship a default.
			continue
		}
		spec, err := formatspec.Parse(def)
		if err != nil {
			t.Errorf("%s: shipped default for %s is malformed: %v", site.Pos, site.Key, err)
			continue
		}
		if spec.Arity() != site.Args {
			t.Errorf("%s: %s is called with %d argument(s) but its shipped default "+
				"takes %d (%q)", site.Pos, site.Key, site.Args, spec.Arity(), def)
		}
	}

	// Never let a skip look like coverage.
	if len(skipped) > 0 {
		t.Logf("%d call site(s) spread a slice and could not be checked:\n  %s",
			len(skipped), strings.Join(skipped, "\n  "))
	}
}

// TestFormattedStringsAlwaysHaveADefault is the guard for a live bug this PR
// found: addedToBatchFormat, batchClearedFormat, batchCountFormat and
// downloadFinishedFormat shipped with no template value and no runtime
// fallback, so their fields were empty on a stock install. Their call sites
// pass arguments unconditionally, so fmt rendered "%!(EXTRA string=FILE.ZIP)"
// onto the caller's screen every time they tagged a file.
//
// Every formatted key must therefore resolve to a non-empty value from either
// the shipped template or config.StringFallbacks.
func TestFormattedStringsAlwaysHaveADefault(t *testing.T) {
	defaults := shippedDefaults(t)
	for _, key := range stringformat.FormattedKeys {
		if defaults[key] != "" || config.StringFallbacks[key] != "" {
			continue
		}
		t.Errorf("formatted key %q has no shipped default and no runtime fallback, "+
			"so the BBS will print %%!(EXTRA ...) wherever it is used; add a value "+
			"to templates/configs/strings.json or to config.StringFallbacks", key)
	}
}

// TestEmptyFormattedStringIsReported checks the validator no longer treats an
// empty formatted value as "not configured". It is configured -- to print
// fmt's error marker.
func TestEmptyFormattedStringIsReported(t *testing.T) {
	defaults := shippedDefaults(t)
	values := map[string]string{"pageNodeListEntry": ""}

	problems := stringformat.Validate(values, nil, defaults)

	var found bool
	for _, p := range problems {
		if p.Key == "pageNodeListEntry" {
			found = true
			if !strings.Contains(p.Detail, "EXTRA") {
				t.Errorf("detail %q does not explain what the caller will see", p.Detail)
			}
		}
	}
	if !found {
		t.Error("an emptied formatted string was not reported")
	}

	// But an empty value with a runtime fallback is fine: the BBS substitutes.
	fallbacks := map[string]string{"pageNodeListEntry": defaults["pageNodeListEntry"]}
	for _, p := range stringformat.Validate(values, fallbacks, defaults) {
		if p.Key == "pageNodeListEntry" {
			t.Errorf("a fallback-covered empty value was reported: %v", p)
		}
	}
}

// TestFormattedDefaultsParse checks that every formatted key's shipped default
// is well-formed, which the unformatted keys deliberately need not be.
func TestFormattedDefaultsParse(t *testing.T) {
	defaults := shippedDefaults(t)
	for _, key := range stringformat.FormattedKeys {
		def, ok := defaults[key]
		if !ok {
			continue
		}
		if _, err := formatspec.Parse(def); err != nil {
			t.Errorf("shipped default for formatted key %s is malformed: %v (%q)", key, err, def)
		}
	}
}

// TestFallbackOnlyKeysAreCheckable covers a gap in the load-time validation:
// a key that lives only in config.StringFallbacks, never in the template, had
// no signature for Validate to compare a sysop's override against, so a
// malformed value for it reached runtime without a warning.
func TestFallbackOnlyKeysAreCheckable(t *testing.T) {
	shipped := shippedDefaults(t)

	// searchResultsHeader is formatted, absent from the template, and covered
	// only by the fallback table -- exactly the shape that used to slip past.
	const key = "searchResultsHeader"
	if _, inTemplate := shipped[key]; inTemplate {
		t.Skipf("%s is now in the template; pick another fallback-only key", key)
	}
	if config.StringFallbacks[key] == "" {
		t.Fatalf("%s is not in StringFallbacks; the test premise is stale", key)
	}

	// Start from a healthy install and break exactly one key, so anything
	// reported is attributable to that edit.
	values := installValues(t, shipped)
	values[key] = "|15Search results|07" // the %s dropped

	reportedFor := func(defaults map[string]string) bool {
		for _, p := range stringformat.Validate(values, config.StringFallbacks, defaults) {
			if p.Key == key {
				return true
			}
		}
		return false
	}

	if reportedFor(shipped) {
		t.Log("already reported against the template alone; the merge is belt and braces")
	}
	if !reportedFor(stringformat.MergeDefaults(config.StringFallbacks, shipped)) {
		t.Errorf("a malformed override of the fallback-only key %q was not reported", key)
	}
}

// TestHealthyInstallReportsNothing checks the validator is silent on a stock
// configuration, so any warning an operator sees is a real one.
func TestHealthyInstallReportsNothing(t *testing.T) {
	shipped := shippedDefaults(t)
	values := installValues(t, shipped)
	defaults := stringformat.MergeDefaults(config.StringFallbacks, shipped)

	if problems := stringformat.Validate(values, config.StringFallbacks, defaults); len(problems) > 0 {
		for _, p := range problems {
			t.Errorf("stock configuration reports: %v", p)
		}
	}
}

// installValues returns what a freshly installed strings.json holds: the
// shipped template, which setup.sh copies into configs/.
func installValues(t *testing.T, shipped map[string]string) map[string]string {
	t.Helper()
	values := make(map[string]string, len(shipped))
	for k, v := range shipped {
		values[k] = v
	}
	return values
}

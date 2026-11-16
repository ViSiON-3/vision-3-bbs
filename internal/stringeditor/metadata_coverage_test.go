package stringeditor

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// legacyCatalogKeys are catalog entries kept for a feature ViSiON/3 does not
// implement. They are listed so a sysop upgrading from Vision/2 can see the
// string is recognised but inert, rather than wondering why it vanished.
var legacyCatalogKeys = map[string]bool{
	"uploadMsgStr": true, // upload-a-message was never ported from Vision/2
}

// runtimeStringKeys returns the json key of every string field the BBS reads.
func runtimeStringKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	rt := reflect.TypeOf(config.StringsConfig{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Type.Kind() != reflect.String {
			continue // e.g. LegacyColorSlots, which is not a strings.json string
		}
		if key, _, _ := strings.Cut(f.Tag.Get("json"), ","); key != "" {
			keys[key] = true
		}
	}
	return keys
}

// catalogKeys returns every editable key the editor lists.
func catalogKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for _, e := range StringEntries() {
		if !isReservedKey(e.Key) {
			keys[e.Key] = true
		}
	}
	return keys
}

// TestCatalogCoversRuntimeStrings is the guard that stops a newly added BBS
// string from being uneditable. Every string field in the runtime config must
// have a catalog entry, or the sysop can only change it by hand-editing JSON —
// which is how 15 live strings, including matrixAccountCannotLogon, ended up
// invisible in the editor.
func TestCatalogCoversRuntimeStrings(t *testing.T) {
	catalog := catalogKeys(t)
	for key := range runtimeStringKeys(t) {
		if !catalog[key] {
			t.Errorf("runtime string %q has no entry in StringEntries(); "+
				"add one to internal/stringeditor/metadata.go", key)
		}
	}
}

// TestCatalogHasNoDeadEntries checks the other direction: a catalog entry that
// names no runtime field can never take effect.
func TestCatalogHasNoDeadEntries(t *testing.T) {
	runtime := runtimeStringKeys(t)
	for _, e := range StringEntries() {
		if isReservedKey(e.Key) {
			continue
		}
		if !runtime[e.Key] && !legacyCatalogKeys[e.Key] {
			t.Errorf("catalog entry %q (%s) names no StringsConfig field; add a "+
				"field or record it in legacyCatalogKeys", e.Key, e.Label)
		}
	}
}

// TestTemplateKeysAreClassified checks that every key in the shipped template
// is editable, so an unexplained key cannot quietly accumulate. (The
// Vision/2 leftovers nothing read, such as Read_Feedback, left the template
// in #1100; doctor lists them in an older strings.json as unknown keys.)
func TestTemplateKeysAreClassified(t *testing.T) {
	catalog := catalogKeys(t)
	for key := range shippedTemplate(t) {
		if catalog[key] {
			continue
		}
		t.Errorf("template key %q is not in the catalog", key)
	}
}

// TestLegacyKeysStillLegacy fails if a recorded legacy catalog entry gains a
// runtime field, so the list cannot go stale.
func TestLegacyKeysStillLegacy(t *testing.T) {
	runtime := runtimeStringKeys(t)
	for key := range legacyCatalogKeys {
		if runtime[key] {
			t.Errorf("%q is recorded as legacy but now has a runtime field; "+
				"remove it from legacyCatalogKeys", key)
		}
	}
}

// TestCatalogNumbersAreStable checks that entry numbering is dense and 1-based,
// which is what makes "string 199" mean the same string regardless of filtering.
func TestCatalogNumbersAreStable(t *testing.T) {
	seen := map[string]bool{}
	for i, e := range StringEntries() {
		if e.Number != i+1 {
			t.Fatalf("entry %d has Number %d, want %d", i, e.Number, i+1)
		}
		if seen[e.Key] {
			t.Errorf("duplicate catalog key %q", e.Key)
		}
		seen[e.Key] = true
	}
}

// TestFallbacksAreEditable checks that every runtime fallback names a string
// the sysop can actually reach in the editor.
func TestFallbacksAreEditable(t *testing.T) {
	catalog := catalogKeys(t)
	for key := range config.StringFallbacks {
		if !catalog[key] {
			t.Errorf("runtime fallback %q has no catalog entry", key)
		}
	}
}

package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestStringFallbacksMatchFields guards the reflection-driven loader: every key
// in the table must name a real StringsConfig field, or its default would be
// silently dropped.
func TestStringFallbacksMatchFields(t *testing.T) {
	known := map[string]bool{}
	rt := reflect.TypeOf(StringsConfig{})
	for i := 0; i < rt.NumField(); i++ {
		key, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if key != "" {
			known[key] = true
		}
	}
	for key := range StringFallbacks {
		if !known[key] {
			t.Errorf("StringFallbacks key %q has no StringsConfig field", key)
		}
	}
}

// TestApplyStringDefaultsFillsOnlyEmpty checks that a configured value is never
// overwritten by its fallback.
func TestApplyStringDefaultsFillsOnlyEmpty(t *testing.T) {
	c := StringsConfig{}
	c.SearchNoResults = "|10mine|07"
	applyStringDefaults(&c)

	if c.SearchNoResults != "|10mine|07" {
		t.Errorf("configured value overwritten: %q", c.SearchNoResults)
	}
	if want := StringFallbacks["searchFilesPrompt"]; c.SearchFilesPrompt != want {
		t.Errorf("empty field not filled: got %q, want %q", c.SearchFilesPrompt, want)
	}
}

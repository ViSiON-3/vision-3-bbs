package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
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
	for key := range StringMissingDefaults {
		if !known[key] {
			t.Errorf("StringMissingDefaults key %q has no StringsConfig field", key)
		}
	}
}

// TestLoadStringsMissingDefaults checks that quoteTopNet gets its default only
// when the key is absent: a sysop who blanks it has turned the banner off.
func TestLoadStringsMissingDefaults(t *testing.T) {
	for _, tc := range []struct {
		name, json, want string
	}{
		{"absent", `{}`, StringMissingDefaults["quoteTopNet"]},
		{"blank", `{"quoteTopNet": ""}`, ""},
		{"set", `{"quoteTopNet": "^N said:"}`, "^N said:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "strings.json"), []byte(tc.json), 0o644); err != nil {
				t.Fatal(err)
			}
			c, err := LoadStrings(dir)
			if err != nil {
				t.Fatal(err)
			}
			if c.QuoteTopNet != tc.want {
				t.Errorf("QuoteTopNet = %q, want %q", c.QuoteTopNet, tc.want)
			}
		})
	}
}

// TestLoadStringsKeepsBlankChatNotices checks that a sysop who blanks a chat
// notice has switched it off, while a board without the key gets the notice.
func TestLoadStringsKeepsBlankChatNotices(t *testing.T) {
	for _, tc := range []struct {
		name, json, want string
	}{
		{"absent", `{}`, StringMissingDefaults["sysopChatSummon"]},
		{"blank", `{"sysopChatSummon": ""}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "strings.json"), []byte(tc.json), 0o644); err != nil {
				t.Fatal(err)
			}
			c, err := LoadStrings(dir)
			if err != nil {
				t.Fatal(err)
			}
			if c.SysopChatSummon != tc.want {
				t.Errorf("SysopChatSummon = %q, want %q", c.SysopChatSummon, tc.want)
			}
		})
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

// TestLoadStringsDoesNotWarnAboutMissingDefaults checks that an upgraded install
// whose strings.json predates the toast strings gets no warning that they are
// empty, since the loader fills them in.
func TestLoadStringsDoesNotWarnAboutMissingDefaults(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "strings.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadStrings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.ToastLogon != StringMissingDefaults["toastLogon"] {
		t.Errorf("ToastLogon = %q, want the missing-key default", c.ToastLogon)
	}
	for _, key := range []string{"toastLogon", "toastLogoff", "toastOverlayFormat"} {
		if strings.Contains(logs.String(), "key="+key+" ") {
			t.Errorf("%s was reported though the loader fills it in", key)
		}
	}
}

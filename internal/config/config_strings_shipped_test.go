package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/stringformat"
	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// stringField returns the value of the field whose json key is key.
func stringField(t *testing.T, c StringsConfig, key string) string {
	t.Helper()
	v := reflect.ValueOf(c)
	for i := 0; i < v.NumField(); i++ {
		if k, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("json"), ","); k == key {
			return v.Field(i).String()
		}
	}
	t.Fatalf("no field for %q", key)
	return ""
}

func shippedStrings(t *testing.T) map[string]string {
	t.Helper()
	shipped, err := configtemplates.StringDefaults()
	if err != nil {
		t.Fatal(err)
	}
	return shipped
}

func writeStrings(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "strings.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// With no strings.json, or an empty one, every key gets the shipped text
// (#1100): the file only has to hold the sysop's own wording.
func TestLoadStringsShippedDefaults(t *testing.T) {
	shipped := shippedStrings(t)
	for name, dir := range map[string]string{
		"no file":    t.TempDir(),
		"empty file": writeStrings(t, "{}"),
	} {
		got, err := LoadStrings(dir)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		v := reflect.ValueOf(got)
		for i := 0; i < v.NumField(); i++ {
			key, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("json"), ",")
			want, ok := shipped[key]
			if !ok || want == "" || v.Field(i).Kind() != reflect.String {
				continue
			}
			if v.Field(i).String() != want {
				t.Errorf("%s: %s = %q; want the shipped %q", name, key, v.Field(i).String(), want)
			}
		}
	}
}

// The file's value wins; a key it leaves out has the shipped text, so a
// changed shipped default reaches it.
func TestLoadStringsFileOverShipped(t *testing.T) {
	shipped := shippedStrings(t)
	dir := writeStrings(t, `{"pauseString": "|15Hit a key"}`)
	got, err := LoadStrings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.PauseString != "|15Hit a key" {
		t.Errorf("pauseString = %q; want the file's", got.PauseString)
	}
	if got.AnonymousName != shipped["anonymousName"] || got.AnonymousName == "" {
		t.Errorf("anonymousName = %q; want the shipped %q", got.AnonymousName, shipped["anonymousName"])
	}
}

// A value whose format directives don't match is replaced by the shipped
// default for the run, and the file is left as it is.
func TestLoadStringsMismatchedFormatUsesDefault(t *testing.T) {
	shipped := shippedStrings(t)
	key := ""
	for _, k := range stringformat.FormattedKeys {
		if def := shipped[k]; strings.Contains(def, "%s") || strings.Contains(def, "%d") {
			if _, missing := StringMissingDefaults[k]; !missing {
				key = k
				break
			}
		}
	}
	if key == "" {
		t.Fatal("no formatted key with arguments in the template")
	}
	content := `{"` + key + `": "broken %d %d %d %d %d %d"}`
	dir := writeStrings(t, content)
	got, err := LoadStrings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v := stringField(t, got, key); v != shipped[key] {
		t.Errorf("%s = %q; want the shipped %q", key, v, shipped[key])
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "strings.json")); string(data) != content {
		t.Errorf("strings.json rewritten: %s", data)
	}
}

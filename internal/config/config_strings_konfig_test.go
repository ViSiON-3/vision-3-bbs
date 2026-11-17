package config

import (
	"reflect"
	"strings"
	"testing"

	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// The User Konfig form's strings are new keys, missing from every
// strings.json written before them. Each needs a fallback, or an upgraded
// board draws the form with blank labels; and the fallback must match the
// shipped template, or F4 in ./strings restores different text from what an
// upgraded board shows.
// blankKonfigStrings are the Konfig strings that ship blank: the key
// legend, since the stock KONFIG.ANS shows how to quit.
var blankKonfigStrings = map[string]bool{"konfigLegend": true}

func TestKonfigStringsHaveFallbacks(t *testing.T) {
	shipped, err := configtemplates.StringDefaults()
	if err != nil {
		t.Fatal(err)
	}
	rt := reflect.TypeOf(StringsConfig{})
	n := 0
	for i := 0; i < rt.NumField(); i++ {
		key, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if !strings.HasPrefix(key, "konfig") {
			continue
		}
		n++
		// Blank by default, so it has no fallback: an absent key, or a
		// blank one, draws nothing.
		if blankKonfigStrings[key] {
			if _, ok := StringFallbacks[key]; ok || shipped[key] != "" {
				t.Errorf("%s should be blank in the template, with no fallback", key)
			}
			continue
		}
		fb, ok := StringFallbacks[key]
		if !ok || fb == "" {
			t.Errorf("%s has no fallback", key)
			continue
		}
		if shipped[key] != fb {
			t.Errorf("%s: template %q differs from fallback %q", key, shipped[key], fb)
		}
	}
	if n == 0 {
		t.Fatal("no konfig* strings defined")
	}
}

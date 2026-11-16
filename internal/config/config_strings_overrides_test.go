package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/stringhistory"
)

func rawStrings(t *testing.T, s string) map[string]json.RawMessage {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

// EffectiveString agrees with LoadStrings for every key, present or not.
func TestEffectiveStringMatchesLoadStrings(t *testing.T) {
	old := StringSuperseded["chatMsgUsage"][0]
	file := map[string]string{"pauseString": "mine", "chatMsgUsage": old, "quoteTopNet": "", "anonymousName": ""}
	data, _ := json.Marshal(file)
	dir := writeStrings(t, string(data))
	loaded, err := LoadStrings(dir)
	if err != nil {
		t.Fatal(err)
	}
	for key := range stringKeys() {
		value, present := file[key]
		if got, want := EffectiveString(key, value, present), stringField(t, loaded, key); got != want {
			t.Errorf("%s: EffectiveString = %q; LoadStrings = %q", key, got, want)
		}
	}
}

// TrimStrings keeps the sysop's own values and drops what shipped, in any
// release, what prints the same as the default, and keys nothing reads.
func TestTrimStrings(t *testing.T) {
	old := StringSuperseded["chatHelp"][0]
	raw := rawStrings(t, `{
		"pauseString": "|15My own pause",
		"anonymousName": `+mustJSON(StringDefault("anonymousName"))+`,
		"chatHelp": `+mustJSON(old)+`,
		"Read_Feedback": "Read Feedback?",
		"defColor1": 12,
		"defColor2": 0,
		"_note": "kept"
	}`)
	tr := TrimStrings(raw)
	if want := []string{"anonymousName", "chatHelp"}; !reflect.DeepEqual(tr.Shipped, want) {
		t.Errorf("shipped = %v; want %v", tr.Shipped, want)
	}
	if want := []string{"Read_Feedback", "defColor2"}; !reflect.DeepEqual(tr.Unknown, want) {
		t.Errorf("unknown = %v; want %v", tr.Unknown, want)
	}
	for _, k := range []string{"pauseString", "defColor1", "_note", StringBasesKey} {
		if _, ok := tr.Kept[k]; !ok {
			t.Errorf("%s not kept: %v", k, tr.Kept)
		}
	}
	if len(tr.Kept) != 4 {
		t.Errorf("kept %d keys; want 4: %v", len(tr.Kept), tr.Kept)
	}
	bases := StringBases(tr.Kept)
	if want := map[string]string{"pauseString": stringhistory.Hash(StringDefault("pauseString"))}; !reflect.DeepEqual(bases, want) {
		t.Errorf("bases = %v; want %v", bases, want)
	}
	if changed := ChangedStringDefaults(tr.Kept); len(changed) != 0 {
		t.Errorf("changed right after trimming: %v", changed)
	}
}

// A recorded fingerprint that no longer matches the default means the
// shipped text changed under the sysop's customisation; one already
// recorded is kept by a later trim.
func TestChangedStringDefaults(t *testing.T) {
	raw := rawStrings(t, `{"pauseString": "mine", "anonymousName": "Nobody", "_defaults": {"pauseString": "0123456789abcdef", "gone": "x"}}`)
	if got := ChangedStringDefaults(raw); !reflect.DeepEqual(got, []string{"pauseString"}) {
		t.Errorf("changed = %v; want [pauseString]", got)
	}
	tr := TrimStrings(raw)
	bases := StringBases(tr.Kept)
	if bases["pauseString"] != "0123456789abcdef" || bases["anonymousName"] == "" || len(bases) != 2 {
		t.Errorf("bases after trim = %v; want pauseString's kept, anonymousName's added, gone dropped", bases)
	}
}

// Blank, for a key whose blank prints the fallback, prints the same as the
// default and goes.
func TestTrimStringsBlankFallback(t *testing.T) {
	for key := range StringFallbacks {
		if !IsStringKey(key) {
			continue
		}
		tr := TrimStrings(rawStrings(t, `{"`+key+`": ""}`))
		if len(tr.Shipped) != 1 || len(tr.Kept) != 0 {
			t.Errorf("%s blank: shipped %v, kept %v; want it removed", key, tr.Shipped, tr.Kept)
		}
		return
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return strings.TrimSpace(string(b))
}

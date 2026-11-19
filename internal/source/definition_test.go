package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefinitionReddit(t *testing.T) {
	d, err := LoadDefinition(t.TempDir(), "reddit")
	if err != nil {
		t.Fatal(err)
	}
	if d.Fetch != "browser" || !d.Threads || d.Thread == nil {
		t.Errorf("fetch/threads = %q/%v/%v", d.Fetch, d.Threads, d.Thread != nil)
	}
	if got := d.ListingURLFor("bbs"); got != "https://old.reddit.com/r/bbs/new/" {
		t.Errorf("ListingURLFor = %q", got)
	}
	if d.Message.MsgIDSuffix != "reddit" || d.Message.Kludge != "REDDIT" || d.Message.From != "u/{author}" {
		t.Errorf("message shape = %+v", d.Message)
	}
}

func TestLoadDefinitionOverride(t *testing.T) {
	dir := t.TempDir()
	writeDef(t, dir, "reddit", strings.Replace(shipped(t, "reddit"), `"msgid_suffix": "reddit"`, `"msgid_suffix": "local"`, 1))
	d, err := LoadDefinition(dir, "reddit")
	if err != nil {
		t.Fatal(err)
	}
	if d.Message.MsgIDSuffix != "local" {
		t.Errorf("configs copy not used: msgid_suffix = %q", d.Message.MsgIDSuffix)
	}
}

func TestDefinitionTargetPattern(t *testing.T) {
	d, err := LoadDefinition(t.TempDir(), "reddit")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CheckTarget("bbs"); err != nil {
		t.Errorf("bbs rejected: %v", err)
	}
	if err := d.CheckTarget("r/bbs"); err == nil {
		t.Error("r/bbs accepted")
	}
}

// minimal is a valid flat definition the rejection cases break one way each.
const minimal = `{
  "name": "news",
  "fetch": "http",
  "listing_url": "https://example.com/{feed}/",
  "content": "main",
  "listing": {"item": "article", "fields": {"id": {"attr": "id"}}},
  "message": {"from": "{author}", "msgid_suffix": "news"}
}`

func TestLoadDefinitionRejects(t *testing.T) {
	cases := []struct {
		name, from, to, want string
	}{
		{"unknown key", `"fetch": "http",`, `"fetch": "http", "fecth": "x",`, "fecth"},
		{"bad fetch", `"fetch": "http"`, `"fetch": "ftp"`, "fetch"},
		{"script on http", `"listing": {"item": "article", "fields": {"id": {"attr": "id"}}}`, `"listing": {"script": "return []"}`, "script"},
		{"threads without thread", `"fetch": "http",`, `"fetch": "http", "threads": true,`, "thread"},
		{"listing_url without feed", `https://example.com/{feed}/`, `https://example.com/all/`, "{feed}"},
		{"bad item selector", `"item": "article"`, `"item": "article[["`, "item"},
		{"bad id_pattern", `"item": "article",`, `"item": "article", "id_pattern": "(",`, "id_pattern"},
		{"empty extraction", `{"item": "article", "fields": {"id": {"attr": "id"}}}`, `{}`, "listing"},
		{"bad name", `"name": "news"`, `"name": "News Feed"`, "name"},
		{"no id field", `"fields": {"id": {"attr": "id"}}`, `"fields": {"title": {"attr": "t"}}`, "fields.id"},
		{"misspelled field", `{"id": {"attr": "id"}}`, `{"id": {"attr": "id"}, "titel": {"attr": "t"}}`, "titel"},
		{"posted_at without time", `{"id": {"attr": "id"}}`, `{"id": {"attr": "id"}, "posted_at": {"attr": "d"}}`, "posted_at"},
		{"selector thread without root", `"fetch": "http",`,
			`"fetch": "http", "threads": true, "thread": {"item": "li", "fields": {"id": {"attr": "id"}}},`, "thread.root"},
		{"root_fields without id", `"fetch": "http",`,
			`"fetch": "http", "threads": true, "thread": {"root": "article", "root_fields": {"body": {"sel": "p", "html": true}}, "item": "li", "fields": {"id": {"attr": "id"}}},`, "root_fields.id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(minimal, c.from) {
				t.Fatalf("case does not apply: %q not in minimal", c.from)
			}
			dir := t.TempDir()
			writeDef(t, dir, "news", strings.Replace(minimal, c.from, c.to, 1))
			_, err := LoadDefinition(dir, "news")
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), "news.json") || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not name the file and %q", err, c.want)
			}
		})
	}
	t.Run("a skip_if field may have any name", func(t *testing.T) {
		dir := t.TempDir()
		writeDef(t, dir, "news", strings.Replace(minimal, `{"id": {"attr": "id"}}`, `{"id": {"attr": "id"}, "sponsored": {"attr": "data-ad", "skip_if": "1"}}`, 1))
		if _, err := LoadDefinition(dir, "news"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("minimal is valid", func(t *testing.T) {
		dir := t.TempDir()
		writeDef(t, dir, "news", minimal)
		if _, err := LoadDefinition(dir, "news"); err != nil {
			t.Fatal(err)
		}
	})
}

func writeDef(t *testing.T, configDir, name, body string) {
	t.Helper()
	dir := filepath.Join(configDir, "sources")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func shipped(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "templates", "configs", "sources", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

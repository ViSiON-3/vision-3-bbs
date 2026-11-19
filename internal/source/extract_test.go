package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func redditDef(t *testing.T) Definition {
	t.Helper()
	d, err := LoadDefinition(t.TempDir(), "reddit")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// parsed reads the Reddit gateway's parse of a fixture, recorded in Item
// shape from internal/reddit before that package was removed.
func parsed(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "parsed_"+strings.TrimSuffix(name, ".html")+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

// The Reddit definition must read the fixtures exactly as the parser it
// replaced did.
func TestExtractListingMatchesReddit(t *testing.T) {
	d := redditDef(t)
	for _, name := range []string{"listing.html", "mini_listing.html"} {
		t.Run(name, func(t *testing.T) {
			var want struct{ Items []Item }
			parsed(t, name, &want)
			got, err := ExtractListing(fixture(t, name), d)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(want.Items) || len(got) == 0 {
				t.Fatalf("%d items, want %d", len(got), len(want.Items))
			}
			for i, w := range want.Items {
				g := got[i]
				if g.ID != w.ID || g.URL != w.URL || g.Thread != w.Thread || g.Title != w.Title ||
					g.Author != w.Author || !g.PostedAt.Equal(w.PostedAt) || g.Marker != w.Marker || g.Link != w.Link {
					t.Errorf("item %d:\n got %+v\nwant %+v", i, g, w)
				}
			}
		})
	}
}

func TestExtractThreadMatchesReddit(t *testing.T) {
	d := redditDef(t)
	for _, name := range []string{"post.html", "mini_post.html"} {
		t.Run(name, func(t *testing.T) {
			var want struct {
				Root    Item
				Replies []Item
			}
			parsed(t, name, &want)
			root, replies, err := ExtractThread(fixture(t, name), d)
			if err != nil {
				t.Fatal(err)
			}
			if root.ID != want.Root.ID || root.Body != want.Root.Body {
				t.Errorf("root = %q body %d bytes, want %q body %d bytes", root.ID, len(root.Body), want.Root.ID, len(want.Root.Body))
			}
			if len(replies) != len(want.Replies) || len(replies) == 0 {
				t.Fatalf("%d replies, want %d", len(replies), len(want.Replies))
			}
			for i, w := range want.Replies {
				g := replies[i]
				if g.ID != w.ID || g.ParentID != w.ParentID || g.Author != w.Author || g.Body != w.Body || !g.PostedAt.Equal(w.PostedAt) {
					t.Errorf("reply %d:\n got %+v\nwant %+v", i, g, w)
				}
			}
		})
	}
}

func TestExtractSkips(t *testing.T) {
	d := redditDef(t)
	page := `<div class="content" role="main">
	<div class="thing" data-type="link" data-fullname="t3_ok" data-promoted="false" data-permalink="/r/x/comments/ok/"><a class="title">kept</a></div>
	<div class="thing" data-type="link" data-fullname="t3_ad" data-promoted="true" data-permalink="/r/x/comments/ad/"><a class="title">advert</a></div>
	<div class="thing" data-type="link" data-fullname="" data-permalink="/r/x/comments/e/"><a class="title">no id</a></div>
	<div class="thing" data-type="link" data-fullname="t1_wrongkind" data-permalink="/r/x/comments/w/"><a class="title">wrong kind</a></div>
	</div>`
	got, err := ExtractListing(page, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "t3_ok" {
		t.Errorf("items = %+v, want only t3_ok", got)
	}
}

func TestExtractThreadWithoutRoot(t *testing.T) {
	if _, _, err := ExtractThread(`<div class="content" role="main"></div>`, redditDef(t)); err == nil {
		t.Error("a page with no root item was accepted")
	}
}

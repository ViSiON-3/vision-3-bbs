package reddit

import (
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func shippedSelectors(t *testing.T) Selectors {
	t.Helper()
	sel, err := LoadSelectors(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return sel
}

func TestParseListingMini(t *testing.T) {
	posts, err := ParseListing(fixture(t, "mini_listing.html"), shippedSelectors(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 {
		t.Fatalf("got %d posts, want 2 (advert and id-less post skipped): %+v", len(posts), posts)
	}
	p := posts[0]
	if p.ID != "t3_aaa111" || p.Title != "First post" || p.Author != "alice" || p.CommentCount != 2 {
		t.Errorf("post 0 = %+v", p)
	}
	if !p.Created.Equal(time.UnixMilli(1790985694000).UTC()) {
		t.Errorf("created = %v", p.Created)
	}
	if p.URL() != "https://www.reddit.com/r/bbs/comments/aaa111/first_post/" {
		t.Errorf("URL = %q", p.URL())
	}
	if posts[1].Title != "A link & more ☃" || posts[1].ContentHref != "https://example.com/page" {
		t.Errorf("post 1 = %+v", posts[1])
	}
}

func TestParsePostPageMini(t *testing.T) {
	post, comments, err := ParsePostPage(fixture(t, "mini_post.html"), shippedSelectors(t))
	if err != nil {
		t.Fatal(err)
	}
	if post.ID != "t3_aaa111" || !strings.Contains(post.BodyHTML, "Hello") {
		t.Errorf("post = %+v", post)
	}
	ids := []string{}
	for _, c := range comments {
		ids = append(ids, c.ID+"<"+c.ParentID)
	}
	want := "t1_c1<t3_aaa111 t1_c2<t1_c1 t1_c3<t3_aaa111 t1_c5<t1_c3"
	if got := strings.Join(ids, " "); got != want {
		t.Fatalf("comments (id<parent) = %s, want %s", got, want)
	}
	if strings.Contains(comments[0].BodyHTML, "Nested reply") {
		t.Errorf("comment body swallowed a reply: %q", comments[0].BodyHTML)
	}
	if comments[1].Created.Hour() != 2 {
		t.Errorf("comment time = %v", comments[1].Created)
	}
}

func TestParsePostPageDeletedComment(t *testing.T) {
	_, comments, err := ParsePostPage(fixture(t, "mini_post.html"), shippedSelectors(t))
	if err != nil {
		t.Fatal(err)
	}
	c := comments[2]
	if c.Author != deletedAuthor || !strings.Contains(c.BodyHTML, "[removed]") {
		t.Errorf("deleted comment = %+v", c)
	}
}

// The real pages captured in Task 2 must parse into something sensible.
func TestParseRealFixtures(t *testing.T) {
	sel := shippedSelectors(t)
	posts, err := ParseListing(fixture(t, "listing.html"), sel)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) < 20 {
		t.Fatalf("only %d posts parsed from listing.html", len(posts))
	}
	for _, p := range posts {
		if !strings.HasPrefix(p.ID, "t3_") || p.Title == "" || p.Created.IsZero() || !strings.HasPrefix(p.Permalink, "/r/") {
			t.Errorf("incomplete post: %+v", p)
		}
	}
	post, comments, err := ParsePostPage(fixture(t, "post.html"), sel)
	if err != nil {
		t.Fatal(err)
	}
	if post.ID == "" || len(comments) == 0 || len(comments) != post.CommentCount {
		t.Fatalf("post.html: post %+v, %d comments", post, len(comments))
	}
	for _, c := range comments {
		if !strings.HasPrefix(c.ID, "t1_") || c.ParentID == "" || c.Created.IsZero() || strings.TrimSpace(c.BodyHTML) == "" {
			t.Errorf("incomplete comment: %+v", c)
		}
	}
}

package reddit

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	redditOrigin  = "https://www.reddit.com"
	deletedAuthor = "[deleted]"
)

// Post is one subreddit post as read from old Reddit.
type Post struct {
	ID           string // t3_...
	Permalink    string // /r/<sub>/comments/...
	Title        string
	Author       string
	ContentHref  string
	BodyHTML     string // only from the post page
	Created      time.Time
	CommentCount int
}

// Comment is one comment as read from an old-Reddit post page.
type Comment struct {
	ID       string // t1_...
	ParentID string // t1_... or the post's t3_...
	Author   string
	BodyHTML string
	Created  time.Time
}

// URL is the post's canonical permalink, shown in messages.
func (p Post) URL() string {
	if strings.HasPrefix(p.Permalink, "http") {
		return p.Permalink
	}
	return redditOrigin + p.Permalink
}

// ParseListing reads every post thing on a listing page. Adverts and things
// without a t3_ id are skipped.
func ParseListing(page string, sel Selectors) ([]Post, error) {
	root, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return nil, fmt.Errorf("parsing listing: %w", err)
	}
	var posts []Post
	walk(root, func(n *html.Node) bool {
		if isThing(n, sel, sel.PostType) {
			if p, ok := readPost(n, sel); ok {
				posts = append(posts, p)
			}
			return false
		}
		return true
	})
	return posts, nil
}

// ParsePostPage reads the post and every comment in page order, which puts
// each parent before its replies. A comment's parent is the nearest enclosing
// comment thing, else the post: old Reddit nests replies in their parent.
func ParsePostPage(page string, sel Selectors) (Post, []Comment, error) {
	root, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return Post{}, nil, fmt.Errorf("parsing post page: %w", err)
	}
	var post Post
	var found bool
	var comments []Comment
	var visit func(n *html.Node, parent string)
	visit = func(n *html.Node, parent string) {
		switch {
		case !found && isThing(n, sel, sel.PostType):
			if p, ok := readPost(n, sel); ok {
				p.BodyHTML = ownHTMLByClass(n, sel.BodyClass, sel)
				post, found = p, true
			}
			return
		case isThing(n, sel, sel.CommentType):
			c, ok := readComment(n, sel)
			if ok {
				c.ParentID = parent
				comments = append(comments, c)
				parent = c.ID
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			visit(ch, parent)
		}
	}
	visit(root, "")
	if !found {
		return Post{}, nil, fmt.Errorf("no %s thing on the page", sel.PostType)
	}
	for i := range comments {
		if comments[i].ParentID == "" {
			comments[i].ParentID = post.ID
		}
	}
	return post, comments, nil
}

func isThing(n *html.Node, sel Selectors, kind string) bool {
	return n.Type == html.ElementNode && n.Data == sel.ThingElement && attr(n, sel.TypeAttr) == kind
}

func readPost(n *html.Node, sel Selectors) (Post, bool) {
	p := Post{
		ID:          attr(n, sel.IDAttr),
		Permalink:   attr(n, sel.PermalinkAttr),
		Author:      attr(n, sel.AuthorAttr),
		ContentHref: attr(n, sel.URLAttr),
		Title:       ownText(n, sel.TitleElement, sel.TitleClass, sel),
	}
	if !strings.HasPrefix(p.ID, "t3_") || attr(n, sel.PromotedAttr) == "true" {
		return Post{}, false
	}
	p.CommentCount, _ = strconv.Atoi(attr(n, sel.CommentCountAttr))
	if ms, err := strconv.ParseInt(attr(n, sel.TimestampMsAttr), 10, 64); err == nil {
		p.Created = time.UnixMilli(ms).UTC()
	}
	if p.Author == "" {
		p.Author = deletedAuthor
	}
	if strings.HasPrefix(p.ContentHref, "/") {
		p.ContentHref = redditOrigin + p.ContentHref
	}
	return p, true
}

func readComment(n *html.Node, sel Selectors) (Comment, bool) {
	c := Comment{
		ID:       attr(n, sel.IDAttr),
		Author:   attr(n, sel.AuthorAttr),
		BodyHTML: ownHTMLByClass(n, sel.BodyClass, sel),
	}
	if !strings.HasPrefix(c.ID, "t1_") {
		return Comment{}, false
	}
	if t := ownElement(n, sel.TimeElement, "", sel); t != nil {
		c.Created, _ = time.Parse(time.RFC3339, attr(t, sel.TimeAttr))
		c.Created = c.Created.UTC()
	}
	if c.Author == "" {
		c.Author = deletedAuthor
	}
	return c, true
}

// ownElement finds the first descendant of thing with the given tag (and
// class, when set) without entering the thing's replies (its child_class div)
// or any nested thing.
func ownElement(thing *html.Node, tag, class string, sel Selectors) *html.Node {
	var found *html.Node
	var search func(*html.Node)
	search = func(m *html.Node) {
		for c := m.FirstChild; c != nil && found == nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if hasClass(c, sel.ChildClass) || attr(c, sel.TypeAttr) != "" {
				continue
			}
			if (tag == "" || c.Data == tag) && (class == "" || hasClass(c, class)) {
				found = c
				return
			}
			search(c)
		}
	}
	search(thing)
	return found
}

// ownHTMLByClass is the inner HTML of the thing's own first element with class.
func ownHTMLByClass(thing *html.Node, class string, sel Selectors) string {
	n := ownElement(thing, "", class, sel)
	if n == nil {
		return ""
	}
	var buf bytes.Buffer
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&buf, c) // writes to a bytes.Buffer
	}
	return buf.String()
}

// ownText is the text of the thing's own first tag.class element.
func ownText(thing *html.Node, tag, class string, sel Selectors) string {
	n := ownElement(thing, tag, class, sel)
	if n == nil {
		return ""
	}
	var b strings.Builder
	walk(n, func(m *html.Node) bool {
		if m.Type == html.TextNode {
			b.WriteString(m.Data)
		}
		return true
	})
	return strings.TrimSpace(b.String())
}

func hasClass(n *html.Node, class string) bool {
	if class == "" {
		return false
	}
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

// walk visits n and its descendants depth first; visit returns false to skip
// a node's children.
func walk(n *html.Node, visit func(*html.Node) bool) {
	if !visit(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, visit)
	}
}

func attr(n *html.Node, name string) string {
	if name == "" {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == name {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

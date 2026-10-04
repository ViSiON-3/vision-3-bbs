package reddit

import (
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	spaceRun    = regexp.MustCompile(`[ \t\r\n]+`)
	blankLines  = regexp.MustCompile(`\n{3,}`)
	spaceBefore = regexp.MustCompile(` +\n`)
	spaceAfter  = regexp.MustCompile(`\n +`)
)

// HTMLToText turns a post or comment body into plain text: paragraphs are
// separated by blank lines, links show their URL, list items get "- ",
// block quotes get "> ", and preformatted text is kept as it is.
func HTMLToText(fragment string) string {
	if strings.TrimSpace(fragment) == "" {
		return ""
	}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return strings.TrimSpace(fragment)
	}
	c := &converter{}
	var b strings.Builder
	for _, n := range nodes {
		c.writeText(&b, n)
	}
	out := spaceBefore.ReplaceAllString(b.String(), "\n")
	out = spaceAfter.ReplaceAllString(out, "\n")
	out = blankLines.ReplaceAllString(out, "\n\n")
	// Preformatted blocks go back in last, so the whitespace cleanup above
	// never touches their indentation.
	for i, pre := range c.pres {
		out = strings.Replace(out, preMarker(i), pre, 1)
	}
	return strings.TrimSpace(out)
}

// converter holds preformatted blocks aside while the rest is cleaned up.
type converter struct {
	pres []string
}

func preMarker(i int) string { return "\x00PRE" + strconv.Itoa(i) + "\x00" }

func (c *converter) writeText(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(spaceRun.ReplaceAllString(n.Data, " "))
		return
	case html.ElementNode:
	default:
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			c.writeText(b, ch)
		}
		return
	}
	switch n.Data {
	case "br":
		b.WriteString("\n")
	case "pre":
		b.WriteString("\n\n" + preMarker(len(c.pres)) + "\n\n")
		c.pres = append(c.pres, rawText(n))
	case "a":
		var inner strings.Builder
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			c.writeText(&inner, ch)
		}
		text := strings.TrimSpace(inner.String())
		href := attr(n, "href")
		switch {
		case href == "" || href == text:
			b.WriteString(text)
		case text == "":
			b.WriteString(href)
		default:
			b.WriteString(text + " <" + href + ">")
		}
	case "li":
		b.WriteString("\n- ")
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			c.writeText(b, ch)
		}
	case "blockquote":
		var inner strings.Builder
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			c.writeText(&inner, ch)
		}
		b.WriteString("\n\n")
		for _, line := range strings.Split(strings.TrimSpace(inner.String()), "\n") {
			b.WriteString("> " + strings.TrimSpace(line) + "\n")
		}
		b.WriteString("\n")
	case "p", "div", "ul", "ol", "h1", "h2", "h3", "h4", "h5", "h6", "table", "tr":
		b.WriteString("\n\n")
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			c.writeText(b, ch)
		}
		b.WriteString("\n\n")
	default:
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			c.writeText(b, ch)
		}
	}
}

// rawText is the text under n with its whitespace untouched.
func rawText(n *html.Node) string {
	var b strings.Builder
	var rec func(*html.Node)
	rec = func(m *html.Node) {
		if m.Type == html.TextNode {
			b.WriteString(m.Data)
		}
		for c := m.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(n)
	return strings.Trim(b.String(), "\n")
}

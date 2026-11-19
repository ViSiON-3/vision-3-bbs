package source

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// ExtractListing reads every item on a listing page with the definition's
// listing selectors. Items without an ID, whose ID fails id_pattern, or with
// a skip_if field that matches, are left out.
func ExtractListing(page string, d Definition) ([]Item, error) {
	root, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return nil, fmt.Errorf("parsing listing: %w", err)
	}
	x, err := compile(d.Listing, d.PageOrigin)
	if err != nil {
		return nil, err
	}
	var items []Item
	walk(root, func(n *html.Node) bool {
		if !x.item.Match(n) {
			return true
		}
		if it, ok := x.read(n, x.fields, x.idPattern); ok {
			items = append(items, it)
		}
		return false
	})
	return items, nil
}

// ExtractThread reads a thread page: its first root item, then every reply
// in page order, which puts each parent before its replies. With parent
// "nearest_item", a reply's parent is the closest enclosing reply, else the
// root.
func ExtractThread(page string, d Definition) (Item, []Item, error) {
	if d.Thread == nil {
		return Item{}, nil, fmt.Errorf("source %s has no thread extraction", d.Name)
	}
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return Item{}, nil, fmt.Errorf("parsing thread page: %w", err)
	}
	x, err := compile(*d.Thread, d.PageOrigin)
	if err != nil {
		return Item{}, nil, err
	}
	var root Item
	var found bool
	var replies []Item
	var visit func(n *html.Node, parent string)
	visit = func(n *html.Node, parent string) {
		switch {
		case !found && x.root != nil && x.root.Match(n):
			if it, ok := x.read(n, x.rootFields, nil); ok {
				root, found = it, true
			}
			return
		case x.item.Match(n):
			if it, ok := x.read(n, x.fields, x.idPattern); ok {
				it.ParentID = parent
				replies = append(replies, it)
				if x.nested {
					parent = it.ID
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c, parent)
		}
	}
	visit(doc, "")
	if !found {
		return Item{}, nil, fmt.Errorf("no %s item on the thread page", d.Thread.Root)
	}
	for i := range replies {
		if replies[i].ParentID == "" {
			replies[i].ParentID = root.ID
		}
	}
	return root, replies, nil
}

// compiled is an Extraction with its selectors compiled.
type compiled struct {
	item, root, exclude cascadia.Matcher
	idPattern           *regexp.Regexp
	nested              bool
	fields, rootFields  map[string]compiledField
	origin              string
}

type compiledField struct {
	Field
	sel cascadia.Matcher
}

func compile(e Extraction, origin string) (*compiled, error) {
	x := &compiled{nested: e.Parent == "nearest_item", origin: origin}
	var err error
	if x.item, err = cascadia.Compile(e.Item); err != nil {
		return nil, fmt.Errorf("item selector %q: %w", e.Item, err)
	}
	if e.Root != "" {
		if x.root, err = cascadia.Compile(e.Root); err != nil {
			return nil, fmt.Errorf("root selector %q: %w", e.Root, err)
		}
	}
	if e.Exclude != "" {
		if x.exclude, err = cascadia.Compile(e.Exclude); err != nil {
			return nil, fmt.Errorf("exclude selector %q: %w", e.Exclude, err)
		}
	}
	if e.IDPattern != "" {
		if x.idPattern, err = regexp.Compile(e.IDPattern); err != nil {
			return nil, fmt.Errorf("id_pattern: %w", err)
		}
	}
	if x.fields, err = compileFields(e.Fields); err != nil {
		return nil, err
	}
	if x.rootFields, err = compileFields(e.RootFields); err != nil {
		return nil, err
	}
	return x, nil
}

func compileFields(fields map[string]Field) (map[string]compiledField, error) {
	out := make(map[string]compiledField, len(fields))
	for name, f := range fields {
		cf := compiledField{Field: f}
		if f.Sel != "" {
			m, err := cascadia.Compile(f.Sel)
			if err != nil {
				return nil, fmt.Errorf("field %s selector %q: %w", name, f.Sel, err)
			}
			cf.sel = m
		}
		out[name] = cf
	}
	return out, nil
}

// read builds an Item from an item element; ok is false when it is skipped.
// A thread's root is read with no pattern: id_pattern describes the replies.
func (x *compiled) read(n *html.Node, fields map[string]compiledField, pattern *regexp.Regexp) (Item, bool) {
	values := map[string]string{}
	var posted time.Time
	for name, f := range fields {
		v := x.value(n, f)
		if f.SkipIf != "" && v == f.SkipIf {
			return Item{}, false
		}
		if name == "posted_at" {
			posted = parseTime(v, f.Time)
			continue
		}
		values[name] = v
	}
	it := Item{
		ID: values["id"], ParentID: values["parent_id"], Title: values["title"], Author: values["author"],
		PostedAt: posted, Body: values["body"], URL: values["url"], Thread: values["thread"], Link: values["link"],
		Marker: values["marker"],
	}
	if it.ID == "" || (pattern != nil && !pattern.MatchString(it.ID)) {
		return Item{}, false
	}
	return it, true
}

// value reads one field from item element n.
func (x *compiled) value(n *html.Node, f compiledField) string {
	target := n
	if f.sel != nil {
		target = x.own(n, f.sel)
	}
	v := ""
	switch {
	case target == nil:
	case f.Attr != "":
		v = attr(target, f.Attr)
	case f.HTML:
		v = innerHTML(target)
	default:
		v = text(target)
	}
	if v == "" {
		v = f.Default
	}
	if v != "" {
		v += f.Suffix
	}
	if f.URL && strings.HasPrefix(v, "/") {
		origin := f.Origin
		if origin == "" {
			origin = x.origin
		}
		v = strings.TrimRight(origin, "/") + v
	}
	return v
}

// own finds the first descendant of item matching m without entering any
// element that matches the exclude selector.
func (x *compiled) own(item *html.Node, m cascadia.Matcher) *html.Node {
	var found *html.Node
	var search func(*html.Node)
	search = func(p *html.Node) {
		for c := p.FirstChild; c != nil && found == nil; c = c.NextSibling {
			if c.Type != html.ElementNode || (x.exclude != nil && x.exclude.Match(c)) {
				continue
			}
			if m.Match(c) {
				found = c
				return
			}
			search(c)
		}
	}
	search(item)
	return found
}

func parseTime(v, kind string) time.Time {
	switch kind {
	case "unix_ms":
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.UnixMilli(ms).UTC()
		}
	case "rfc3339":
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func innerHTML(n *html.Node) string {
	var buf bytes.Buffer
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&buf, c) // writes to a bytes.Buffer
	}
	return buf.String()
}

func text(n *html.Node) string {
	var b strings.Builder
	walk(n, func(m *html.Node) bool {
		if m.Type == html.TextNode {
			b.WriteString(m.Data)
		}
		return true
	})
	return strings.TrimSpace(b.String())
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
	for _, a := range n.Attr {
		if a.Key == name {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

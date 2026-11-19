// Package source imports web forums and news sites into message areas. A
// source definition file describes one site: how to fetch its pages, how to
// read items out of them and whether items thread. No code here knows about
// any particular site.
package source

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"

	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// Definition is configs/sources/<name>.json.
type Definition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Fetch is "browser" (the sysop's debug Chrome) or "http" (a plain GET).
	Fetch   string `json:"fetch"`
	Threads bool   `json:"threads"`
	// Optional per-source pacing; nil preserves the board's global delay.
	PageDelaySeconds *int `json:"page_delay_seconds,omitempty"`
	// ListingURL is the page listing a feed's newest items; {feed} is
	// replaced with the feed's target.
	ListingURL string `json:"listing_url"`
	// TargetPattern, when set, is a regexp every feed target must match.
	TargetPattern string `json:"target_pattern,omitempty"`
	// PageOrigin is joined to relative URLs.
	PageOrigin string `json:"page_origin"`
	// Ready is waited for before a browser page is read.
	Ready string `json:"ready"`
	// Content is the part of the page that is kept. Nothing outside it
	// leaves the browser: the rest can carry the account's session.
	Content string       `json:"content"`
	Listing Extraction   `json:"listing"`
	Thread  *Extraction  `json:"thread,omitempty"`
	Message MessageShape `json:"message"`
}

// Extraction says how to read items off one kind of page: with CSS
// selectors, or with a script run in the page that returns []Item as JSON.
type Extraction struct {
	// json reads a single HTTP JSON detail object using root_fields paths.
	Format string `json:"format,omitempty"`
	Item   string `json:"item,omitempty"`
	// Root, on a thread page, is the thread's own first item; RootFields
	// are read from it.
	Root       string           `json:"root,omitempty"`
	RootFields map[string]Field `json:"root_fields,omitempty"`
	// Exclude marks elements field lookups never look inside, such as
	// nested items and reply containers.
	Exclude   string `json:"exclude,omitempty"`
	IDPattern string `json:"id_pattern,omitempty"`
	// Parent is "nearest_item" when a reply's parent is the closest
	// enclosing item, and empty when items are flat.
	Parent string           `json:"parent,omitempty"`
	Fields map[string]Field `json:"fields,omitempty"`
	Script string           `json:"script,omitempty"`
}

// Field reads one value from an item element.
type Field struct {
	Path     string `json:"path,omitempty"`     // JSON Pointer, for JSON detail fields
	Suffix   string `json:"suffix,omitempty"`   // appended to a nonempty value
	Required bool   `json:"required,omitempty"` // JSON detail must contain a nonempty value
	Attr     string `json:"attr,omitempty"`     // attribute of the item, or of Sel's match
	Sel      string `json:"sel,omitempty"`      // first matching descendant, outside Exclude
	Text     bool   `json:"text,omitempty"`     // text content of Sel's match
	HTML     bool   `json:"html,omitempty"`     // inner HTML of Sel's match
	Time     string `json:"time,omitempty"`     // "unix_ms" or "rfc3339"
	URL      bool   `json:"url,omitempty"`      // resolve a relative URL against Origin or page_origin
	// Origin overrides page_origin for this field.
	Origin  string `json:"origin,omitempty"`
	SkipIf  string `json:"skip_if,omitempty"` // the item is skipped when the value equals this
	Default string `json:"default,omitempty"`
}

// MessageShape is how an item becomes a message.
type MessageShape struct {
	From         string `json:"from"`          // {author} placeholder
	ReplySubject string `json:"reply_subject"` // {root_title} placeholder
	MsgIDSuffix  string `json:"msgid_suffix"`  // MSGID is "<id>@<suffix>"
	Kludge       string `json:"kludge"`        // "<kludge>: <url>" is added
	Footer       bool   `json:"footer"`        // the URL ends the body
	// BodyFallback names the field used when the body is empty.
	BodyFallback string `json:"body_fallback"`
}

var definitionName = regexp.MustCompile(`^[a-z0-9_-]+$`)

// LoadDefinition reads configs/sources/<name>.json from configDir, or the
// shipped copy when the board has none, and checks it.
func LoadDefinition(configDir, name string) (Definition, error) {
	var d Definition
	file := "sources/" + name + ".json"
	if !definitionName.MatchString(name) {
		return d, fmt.Errorf("%s: name %q must be lowercase letters, digits, - or _", file, name)
	}
	data, err := os.ReadFile(filepath.Join(configDir, "sources", name+".json"))
	if errors.Is(err, os.ErrNotExist) {
		data, err = configtemplates.FS.ReadFile(file)
		if errors.Is(err, os.ErrNotExist) {
			return d, fmt.Errorf("no source definition %s in %s or the shipped templates", file, configDir)
		}
	}
	if err != nil {
		return d, fmt.Errorf("reading %s: %w", file, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return d, fmt.Errorf("parsing %s: %w", file, err)
	}
	if err := d.check(name); err != nil {
		return d, fmt.Errorf("%s: %w", file, err)
	}
	return d, nil
}

func (d Definition) check(name string) error {
	if d.Name != name {
		return fmt.Errorf("name %q must match the file name %q", d.Name, name)
	}
	if d.Fetch != "browser" && d.Fetch != "http" {
		return fmt.Errorf(`fetch is %q; it must be "browser" or "http"`, d.Fetch)
	}
	if d.PageDelaySeconds != nil && (*d.PageDelaySeconds < 0 || *d.PageDelaySeconds > 300) {
		return errors.New("page_delay_seconds must be 0..300")
	}
	if !strings.Contains(d.ListingURL, "{feed}") {
		return errors.New("listing_url must contain {feed}")
	}
	if d.TargetPattern != "" {
		if _, err := regexp.Compile(d.TargetPattern); err != nil {
			return fmt.Errorf("target_pattern: %w", err)
		}
	}
	for what, sel := range map[string]string{"ready": d.Ready, "content": d.Content} {
		if err := checkSelector(what, sel); err != nil {
			return err
		}
	}
	if d.Threads && d.Thread == nil {
		return errors.New("threads is true but there is no thread extraction")
	}
	if d.Threads && d.Thread.Script == "" && d.Thread.Format != "json" && d.Thread.Root == "" {
		return errors.New("thread.root is required: it selects the thread's own item on its page")
	}
	if err := d.Listing.check("listing", d.Fetch); err != nil {
		return err
	}
	if d.Thread != nil {
		if err := d.Thread.check("thread", d.Fetch); err != nil {
			return err
		}
	}
	if d.Message.MsgIDSuffix == "" {
		return errors.New("message.msgid_suffix is required")
	}
	return nil
}

func (e Extraction) check(what, fetch string) error {
	if e.Format != "" && e.Format != "json" {
		return fmt.Errorf("%s.format must be json or omitted", what)
	}
	if e.Format == "json" {
		if what != "thread" || fetch != "http" || e.Script != "" || e.Item != "" || e.Root != "" || e.Exclude != "" || e.Parent != "" || len(e.Fields) != 0 || len(e.RootFields) == 0 {
			return errors.New("JSON details require HTTP thread.root_fields, without HTML selectors or scripts")
		}
	}
	if e.Script != "" {
		if fetch != "browser" {
			return fmt.Errorf("%s.script needs fetch \"browser\"", what)
		}
		return nil
	}
	if e.Format != "json" && (e.Item == "" || len(e.Fields) == 0) {
		return fmt.Errorf("%s needs item and fields, or a script", what)
	}
	for sub, sel := range map[string]string{"item": e.Item, "root": e.Root, "exclude": e.Exclude} {
		if err := checkSelector(what+"."+sub, sel); err != nil {
			return err
		}
	}
	if e.IDPattern != "" {
		if _, err := regexp.Compile(e.IDPattern); err != nil {
			return fmt.Errorf("%s.id_pattern: %w", what, err)
		}
	}
	if e.Parent != "" && e.Parent != "nearest_item" {
		return fmt.Errorf(`%s.parent is %q; it must be "nearest_item" or empty`, what, e.Parent)
	}
	for _, set := range []struct {
		key    string
		fields map[string]Field
	}{{"fields", e.Fields}, {"root_fields", e.RootFields}} {
		if len(set.fields) > 0 {
			if _, ok := set.fields["id"]; !ok {
				return fmt.Errorf("%s.%s.id is required: items without an ID are skipped", what, set.key)
			}
		}
		for name, f := range set.fields {
			where := fmt.Sprintf("%s.%s.%s", what, set.key, name)
			if !itemFields[name] && f.SkipIf == "" {
				return fmt.Errorf("%s is not an item field (%s); a field with another name only works with skip_if", where, itemFieldNames)
			}
			if name == "posted_at" && f.Time == "" {
				return fmt.Errorf(`%s needs time: "unix_ms" or "rfc3339"`, where)
			}
			if e.Format == "json" {
				if _, err := jsonPointerParts(f.Path); err != nil {
					return fmt.Errorf("%s.path: %w", where, err)
				}
				if f.Sel != "" || f.Attr != "" {
					return fmt.Errorf("%s: JSON fields use path, not sel/attr", where)
				}
			} else if f.Path != "" || f.Required {
				return fmt.Errorf("%s.path/required requires JSON format", where)
			}
			if err := checkSelector(where+".sel", f.Sel); err != nil {
				return err
			}
			if f.Time != "" && f.Time != "unix_ms" && f.Time != "rfc3339" {
				return fmt.Errorf(`%s.time is %q; it must be "unix_ms" or "rfc3339"`, where, f.Time)
			}
		}
	}
	return nil
}

// PageDelay returns the site's override, or the board's existing delay.
func (d Definition) PageDelay(fallback time.Duration) time.Duration {
	if d.PageDelaySeconds != nil {
		return time.Duration(*d.PageDelaySeconds) * time.Second
	}
	return fallback
}

// itemFields are the fields an extraction can set on an Item.
var itemFields = map[string]bool{"id": true, "parent_id": true, "title": true, "author": true, "posted_at": true,
	"body": true, "url": true, "thread": true, "link": true, "marker": true}

const itemFieldNames = "id, parent_id, title, author, posted_at, body, url, thread, link, marker"

func checkSelector(what, sel string) error {
	if sel == "" {
		return nil
	}
	if _, err := cascadia.Compile(sel); err != nil {
		return fmt.Errorf("%s selector %q: %w", what, sel, err)
	}
	return nil
}

// ListingURLFor fills a feed's target into the listing URL.
func (d Definition) ListingURLFor(feed string) string {
	return strings.ReplaceAll(d.ListingURL, "{feed}", feed)
}

// CheckTarget reports whether a feed target fits the definition's
// target_pattern.
func (d Definition) CheckTarget(target string) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("target is empty")
	}
	if d.TargetPattern == "" {
		return nil
	}
	if !regexp.MustCompile(d.TargetPattern).MatchString(target) {
		return fmt.Errorf("target %q does not match %s's target_pattern %s", target, d.Name, d.TargetPattern)
	}
	return nil
}

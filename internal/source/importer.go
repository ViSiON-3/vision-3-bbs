package source

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// Feed points one source at one target on the site and files it into one
// message area.
type Feed struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	AreaTag string `json:"area_tag"`
	Enabled bool   `json:"enabled"`
	// Replies, when false, imports items and none of their replies. Absent
	// means on.
	Replies *bool `json:"replies,omitempty"`
	// MaxReplies, when set, keeps only a thread's first replies to the item
	// itself, in page order, and none of their replies. It is for busy
	// threads that run to thousands of replies.
	MaxReplies int `json:"max_replies,omitempty"`
}

// WantReplies reports whether the feed's replies are imported.
func (f Feed) WantReplies() bool { return f.Replies == nil || *f.Replies }

// ErrRunTimeLimit stops a run that could not finish another page load before
// its deadline. What was imported stays; the next run continues from there.
var ErrRunTimeLimit = errors.New("stopped at the run time limit; the next run continues")

// Importer files one feed's new items and replies into a message area.
type Importer struct {
	MM      *message.MessageManager
	Fetcher Fetcher
	Def     Definition
	Store   *Store
	// PageDelay is the pause between page loads, and PageTimeout bounds one.
	PageDelay, PageTimeout time.Duration
	MaxItems               int                 // newest listing items looked at per sync
	Sleep                  func(time.Duration) // time.Sleep in production
	Now                    func() time.Time    // time.Now when nil
	// Deadline, when set, is when the run must be finished: no page load
	// starts unless the pause before it plus PageTimeout fits before it.
	Deadline time.Time
	DryRun   bool
	Out      io.Writer // where a dry run prints
	fetched  int
}

func (im *Importer) now() time.Time {
	if im.Now != nil {
		return im.Now()
	}
	return time.Now()
}

func (im *Importer) msgID(itemID string) string { return itemID + "@" + im.Def.Message.MsgIDSuffix }

// pace waits out the delay before a page load, or refuses the load when it
// could not finish before the deadline.
func (im *Importer) pace(res *FeedResult) error {
	pause := time.Duration(0)
	if im.fetched > 0 {
		pause = im.PageDelay
	}
	if !im.Deadline.IsZero() && im.now().Add(pause+im.PageTimeout).After(im.Deadline) {
		return ErrRunTimeLimit
	}
	if pause > 0 && im.Sleep != nil {
		im.Sleep(pause)
	}
	im.fetched++
	res.Pages++
	return nil
}

// SyncFeed imports what is new in one feed.
func (im *Importer) SyncFeed(f Feed) FeedResult {
	res := FeedResult{Source: f.Source, Feed: f.Target, AreaTag: f.AreaTag}
	area, ok := im.MM.GetAreaByTag(f.AreaTag)
	if !ok {
		res.Err = fmt.Errorf("message area %s does not exist", f.AreaTag)
		return res
	}
	if err := im.pace(&res); err != nil {
		res.Err = err
		return res
	}
	items, err := Listing(im.Fetcher, im.Def, f.Target)
	if err != nil {
		res.Err = err
		return res
	}
	// Newest first, so the cap keeps the newest; then written oldest first,
	// so message numbers in the area follow posting order. The sort is
	// stable: items without a time keep their place on the page.
	sort.SliceStable(items, func(i, j int) bool { return items[i].PostedAt.After(items[j].PostedAt) })
	if im.MaxItems > 0 && len(items) > im.MaxItems {
		items = items[:im.MaxItems]
	}
	for i := len(items) - 1; i >= 0; i-- {
		if err := im.syncItem(area, f, items[i], &res); err != nil {
			res.Err = err
			return res
		}
	}
	return res
}

// syncItem imports an item and whatever of its replies are new.
func (im *Importer) syncItem(area *message.MessageArea, f Feed, it Item, res *FeedResult) error {
	rootID := im.msgID(it.ID)
	known := im.MM.FindMessagesByMSGID(area.ID, []string{rootID})
	seen, wasSeen, err := im.Store.Marker(f.Source, f.Target, it.ID)
	if err != nil {
		return err
	}
	isNew := known[rootID] == 0
	if !isNew && wasSeen && !threadChanged(f, seen, it.Marker) {
		return nil
	}

	root, replies := it, []Item(nil)
	if im.Def.Threads && (isNew || hasReplies(it.Marker)) {
		if err := im.pace(res); err != nil {
			return err
		}
		page, rs, err := Thread(im.Fetcher, im.Def, it.Thread)
		if err != nil {
			return err
		}
		if im.Def.Thread.Format == "json" && page.ID != it.ID {
			return fmt.Errorf("detail ID %q does not match listing ID %q", page.ID, it.ID)
		}
		root.Body, replies = page.Body, keepReplies(f, rs, it.ID)
		if root.Author == "" {
			root.Author = page.Author
		}
		if root.PostedAt.IsZero() {
			root.PostedAt = page.PostedAt
		}
	}

	// Look every ID up before opening the base for writing: the MSGID index
	// opens its own handle on the base and must not nest inside ours.
	ids := []string{rootID}
	for _, r := range replies {
		ids = append(ids, im.msgID(r.ID))
	}
	known = im.MM.FindMessagesByMSGID(area.ID, ids)

	if im.DryRun {
		im.dryRun(f, root, replies, known, res)
		return nil
	}

	base, err := im.MM.GetBase(area.ID)
	if err != nil {
		return err
	}
	defer func() { _ = base.Close() }() // the writes have been checked individually

	if known[rootID] == 0 {
		num, err := im.write(base, im.from(f, root, root), root.Title, im.rootBody(root), rootID, "", 0, root.PostedAt, root.URL)
		if err != nil {
			return err
		}
		known[rootID] = num
		res.Items++
	}
	subject := im.fill(im.Def.Message.ReplySubject, f, root, root)
	for _, r := range replies {
		id := im.msgID(r.ID)
		if known[id] != 0 {
			continue
		}
		parentID := im.msgID(r.ParentID)
		if r.ParentID == "" || known[parentID] == 0 {
			parentID = rootID // parent removed or not on the page
		}
		num, err := im.write(base, im.from(f, r, root), subject, im.footer(HTMLToText(r.Body), root.URL), id, parentID, known[parentID], r.PostedAt, root.URL)
		if err != nil {
			return err
		}
		known[id] = num
		res.Replies++
	}
	return im.Store.SetMarker(f.Source, f.Target, it.ID, it.Marker)
}

// threadChanged reports whether a known thread whose marker was seen and is
// now marker needs its page loaded again. Markers that are both integers,
// such as comment counts, count as changed only when they rise; a capped
// thread is loaded again only while it had fewer replies than the cap.
func threadChanged(f Feed, seen, marker string) bool {
	if !f.WantReplies() {
		return false
	}
	s, errS := strconv.Atoi(seen)
	n, errN := strconv.Atoi(marker)
	if errS != nil || errN != nil {
		return seen != marker
	}
	if f.MaxReplies > 0 {
		return s < f.MaxReplies && n > s
	}
	return n > s
}

// hasReplies reports whether a marker says a known thread has replies to
// load: anything but empty or a zero count.
func hasReplies(marker string) bool {
	m := strings.TrimSpace(marker)
	return m != "" && m != "0"
}

// keepReplies is the part of a thread's replies the feed imports.
func keepReplies(f Feed, rs []Item, rootID string) []Item {
	if !f.WantReplies() {
		return nil
	}
	if f.MaxReplies == 0 {
		return rs
	}
	var top []Item
	for _, r := range rs {
		if (r.ParentID == rootID || r.ParentID == "") && len(top) < f.MaxReplies {
			top = append(top, r)
		}
	}
	return top
}

// fill replaces the message shape's placeholders: {author} (of it),
// {root_title} and {feed}.
func (im *Importer) fill(s string, f Feed, it, root Item) string {
	return strings.NewReplacer("{author}", it.Author, "{root_title}", root.Title, "{feed}", f.Target).Replace(s)
}

func (im *Importer) from(f Feed, it, root Item) string {
	return im.fill(im.Def.Message.From, f, it, root)
}

// rootBody is the item's text, or the field named by body_fallback when the
// text is empty and that field is not the item's own URL, then the footer.
func (im *Importer) rootBody(it Item) string {
	text := HTMLToText(it.Body)
	if text == "" {
		if alt := itemField(it, im.Def.Message.BodyFallback); alt != "" && alt != it.URL {
			text = alt
		}
	}
	return im.footer(text, it.URL)
}

// itemField reads a text field of an item by its definition name.
func itemField(it Item, name string) string {
	switch name {
	case "link":
		return it.Link
	case "url":
		return it.URL
	case "thread":
		return it.Thread
	case "title":
		return it.Title
	case "author":
		return it.Author
	}
	return ""
}

func (im *Importer) footer(text, url string) string {
	if !im.Def.Message.Footer || url == "" {
		return text
	}
	if text == "" {
		return url
	}
	return text + "\n\n" + url
}

func (im *Importer) dryRun(f Feed, root Item, replies []Item, known map[string]int, res *FeedResult) {
	if known[im.msgID(root.ID)] == 0 {
		res.Items++
		_, _ = fmt.Fprintf(im.Out, "item    %s  %s  %s\n", root.ID, im.from(f, root, root), root.Title)
	}
	for _, r := range replies {
		if known[im.msgID(r.ID)] == 0 {
			res.Replies++
			_, _ = fmt.Fprintf(im.Out, "reply   %s  %s  -> %s\n", r.ID, im.from(f, r, root), r.ParentID)
		}
	}
}

func (im *Importer) write(base *jam.Base, from, subject, body, msgID, replyID string, replyTo int, posted time.Time, url string) (int, error) {
	jm := jam.NewMessage()
	jm.From = from
	jm.To = message.MsgToUserAll
	jm.Subject = subject
	jm.Text = body
	jm.DateTime = posted
	if jm.DateTime.IsZero() {
		jm.DateTime = time.Now()
	}
	jm.MsgID = msgID
	jm.ReplyID = replyID
	if replyTo > 0 {
		jm.ReplyTo = uint32(replyTo)
	}
	jm.Kludges = append(jm.Kludges, "CHRS: UTF-8 4")
	if k := im.Def.Message.Kludge; k != "" && url != "" {
		jm.Kludges = append(jm.Kludges, k+": "+url)
	}
	return base.WriteMessage(jm)
}

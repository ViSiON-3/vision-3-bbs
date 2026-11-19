package nntp

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// State stores, per group, the number of the last article imported.
type State interface {
	Marker(source, feed, itemID string) (string, bool, error)
	SetMarker(source, feed, itemID, marker string) error
}

// Importer copies new articles from Reader into message areas. With DryRun
// set it writes nothing and lists what it would import on Out.
type Importer struct {
	Config   Config
	Reader   Reader
	Messages *message.MessageManager
	State    State
	DryRun   bool
	Out      io.Writer
}

// Result is the outcome of syncing one group: the counts, the last article
// number processed and the error that stopped it, if any.
type Result struct {
	Group, AreaTag    string
	Imported, Skipped int
	Through           int64
	Err               error
}

// Sync imports the articles in g that arrived since the last run, threading
// replies under the messages they answer.
func (im *Importer) Sync(g Group) (result Result) {
	result.Group, result.AreaTag = g.Name, g.AreaTag
	fail := func(err error) Result { result.Err = err; return result }
	area, ok := im.Messages.GetAreaByTag(g.AreaTag)
	if !ok {
		return fail(fmt.Errorf("message area %s does not exist", g.AreaTag))
	}
	// Prevent an imported article from leaking onto an existing mail network,
	// or callers replying locally to a group with no outbound NNTP path.
	if area.AreaType != "local" || !strings.EqualFold(area.ACSWrite, "S256") {
		return fail(fmt.Errorf("NNTP target %s must be a local read-only area (acs_write S256)", g.AreaTag))
	}
	low, high, err := im.Reader.Group(g.Name)
	if err != nil {
		return fail(err)
	}
	if high < low || high == 0 {
		return result
	}
	feed := im.Config.Server + "/" + g.Name
	previous, seen, err := im.State.Marker("nntp", feed, "high_water")
	if err != nil {
		return fail(err)
	}
	start := high - int64(im.Config.InitialArticles) + 1
	if seen {
		cursor, e := strconv.ParseInt(previous, 10, 64)
		if e != nil || cursor < 0 {
			return fail(fmt.Errorf("invalid NNTP checkpoint for %s", g.Name))
		}
		if cursor <= high {
			if cursor == high {
				return result
			}
			start = cursor + 1
		}
	}
	if start < low {
		start = low
	}
	if start < 1 {
		start = 1
	}
	end := high
	if high-start >= int64(im.Config.MaxArticles) {
		end = start + int64(im.Config.MaxArticles) - 1
	}
	if end < start || end == math.MaxInt64 {
		return fail(fmt.Errorf("invalid NNTP article range"))
	}
	overview, err := im.Reader.Overview(start, end)
	if err != nil {
		return fail(err)
	}
	type pending struct {
		article Article
		number  int64
	}
	var incoming []pending
	var ids []string
	for _, row := range overview {
		if row.Bytes > int64(im.Config.MaxArticleBytes) {
			result.Skipped++
			continue
		}
		raw, e := im.Reader.Article(row.Number)
		if errors.Is(e, ErrMissing) {
			result.Skipped++
			continue
		}
		if e != nil {
			return fail(e)
		}
		a, e := ParseArticle(raw, im.Config.MaxArticleBytes)
		if e != nil {
			result.Skipped++
			if im.Out != nil {
				_, _ = fmt.Fprintf(im.Out, "%s: skipped article %d: %v\n", g.Name, row.Number, e)
			}
			continue
		}
		if row.ID != "" && a.ID != row.ID {
			return fail(fmt.Errorf("article %d Message-ID differs from overview", row.Number))
		}
		incoming = append(incoming, pending{a, row.Number})
		ids = append(ids, a.ID)
		ids = append(ids, a.References...)
	}
	// Resolve IDs before opening the writable JAM handle: the index takes its
	// own handle. Include all parents once, and extend the map as we write.
	known := im.Messages.FindMessagesByMSGID(area.ID, ids)
	var base *jam.Base
	if !im.DryRun && len(incoming) > 0 {
		base, err = im.Messages.GetBase(area.ID)
		if err != nil {
			return fail(err)
		}
		defer func() { _ = base.Close() }()
	}
	for _, item := range incoming {
		a := item.article
		if known[a.ID] != 0 {
			continue
		}
		if im.DryRun {
			if im.Out != nil {
				_, _ = fmt.Fprintf(im.Out, "article %d  %s  %s\n", item.number, a.From, a.Subject)
			}
			known[a.ID] = -1
			result.Imported++
			continue
		}
		m := jam.NewMessage()
		m.From = a.From
		m.To = message.MsgToUserAll
		m.Subject = a.Subject
		m.Text = a.Body
		m.MsgID = a.ID
		m.DateTime = a.Date
		if m.DateTime.IsZero() {
			m.DateTime = time.Now()
		}
		m.Kludges = []string{"CHRS: UTF-8 4", "NNTP-GROUP: " + g.Name, "NNTP-SERVER: " + im.Config.Server}
		if len(a.References) > 0 {
			m.ReplyID = a.References[len(a.References)-1]
			m.ReplyTo = uint32(known[m.ReplyID])
			m.Kludges = append(m.Kludges, "NNTP-REFERENCES: "+strings.Join(a.References, " "))
		}
		num, e := base.WriteMessage(m)
		if e != nil {
			return fail(e)
		}
		known[a.ID] = num
		result.Imported++
	}
	// Advance across expired holes and deliberately skipped binary/malformed
	// articles only after every accepted message has been stored successfully.
	result.Through = end
	if !im.DryRun {
		if err = im.State.SetMarker("nntp", feed, "high_water", strconv.FormatInt(end, 10)); err != nil {
			return fail(err)
		}
	}
	return result
}

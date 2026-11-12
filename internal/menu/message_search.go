package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/msgsearch"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/term"
)

// SEARCHMSGS: find text in message subjects and bodies across the current
// conference, the current area or every conference. See internal/msgsearch
// for the query syntax and docs/sysop/reference/menu-commands.md.
//
// Searching and listing results never touch newscan tags or last-read
// pointers. Opening a result runs the ordinary message reader on that
// area, stepping only between the messages that match; the area's last-read
// pointer is put back when the reader returns, so a search never changes
// what the next newscan shows.

// msgSearchScope is which areas a search covers, from the command's data.
type msgSearchScope int

const (
	msgSearchConference msgSearchScope = iota // blank or G: the current conference
	msgSearchArea                             // C: the current area
	msgSearchAll                              // A: every conference
)

// parseMsgSearchScope reads the SEARCHMSGS data value. Anything other than
// C or A (in either case) is the current conference.
func parseMsgSearchScope(args string) msgSearchScope {
	switch strings.ToUpper(strings.TrimSpace(args)) {
	case "C":
		return msgSearchArea
	case "A":
		return msgSearchAll
	}
	return msgSearchConference
}

// msgSearchMaxResults caps how many matches one search collects, so a broad
// query on a big board stays quick and small. The results header says when
// the cap was reached.
const msgSearchMaxResults = 500

// msgSearchCheckEvery is how many messages the search reads between checks
// for the caller pressing a key to stop it.
const msgSearchCheckEvery = 25

// msgSearchHit is one matching message.
type msgSearchHit struct {
	areaID   int
	confTag  string
	areaTag  string
	msgNum   int
	subject  string
	from     string
	dateTime time.Time
}

// msgSearchAllowed reports whether the caller may read area: its read ACS
// and its conference's ACS both pass. An area whose conference is not
// configured is judged by its own ACS, as the area lists do.
func msgSearchAllowed(c *cmdCtx, area *message.MessageArea) bool {
	if !checkACS(area.ACSRead, c.currentUser, c.s, c.terminal, c.sessionStartTime) {
		return false
	}
	if area.ConferenceID != 0 && c.e.ConferenceMgr != nil {
		if conf, ok := c.e.ConferenceMgr.GetByID(area.ConferenceID); ok &&
			!checkACS(conf.ACS, c.currentUser, c.s, c.terminal, c.sessionStartTime) {
			return false
		}
	}
	return true
}

// msgSearchAreas returns the areas a search in scope covers, in board order,
// leaving out every area the caller may not read.
func msgSearchAreas(c *cmdCtx, scope msgSearchScope) []*message.MessageArea {
	u := c.currentUser
	var out []*message.MessageArea
	for _, area := range c.e.MessageMgr.ListAreas() {
		switch scope {
		case msgSearchArea:
			if area.ID != u.CurrentMessageAreaID {
				continue
			}
		case msgSearchConference:
			if area.ConferenceID != u.CurrentMsgConferenceID {
				continue
			}
		}
		if msgSearchAllowed(c, area) {
			out = append(out, area)
		}
	}
	return out
}

// msgSearchFilters are the filters a search applies to each message before
// matching it: not deleted, and visible to the caller under the same
// private-mail and twit rules the reader and the message list use. A
// message they reject is never matched, listed or counted. header is the
// part a message's header decides (deleted, private mail), so the search
// can skip reading the body of a message it rejects; visible is all of it.
func msgSearchFilters(c *cmdCtx) (header, visible msgOwnershipFilter) {
	private := withPrivacy(privateMailReaderFor(c.e, c.userManager, c.currentUser), nil)
	header = func(m *message.DisplayMessage) bool {
		return !m.IsDeleted && private(m)
	}
	matcher := twitMatcher(c.e, c.userManager, c.currentUser)
	return header, withTwit(func() *user.TwitMatcher { return matcher }, header, nil)
}

// searchMessages finds the messages in areas that visible accepts and q
// matches, at most msgSearchMaxResults of them. header sees each message
// with only its header read first, and a message it rejects is skipped
// without reading its body; visible must reject it too. stop is called
// between areas and every msgSearchCheckEvery messages; when it returns
// true the search ends with what it has. full reports that the cap was
// reached, and stopped that stop ended it. An area whose base can't be
// read is logged and skipped.
func searchMessages(e *MenuExecutor, areas []*message.MessageArea, q *msgsearch.Query,
	header, visible msgOwnershipFilter, stop func() bool) (hits []msgSearchHit, full, stopped bool) {
	confTags := map[int]string{}
	if e.ConferenceMgr != nil {
		for _, conf := range e.ConferenceMgr.ListConferences() {
			confTags[conf.ID] = conf.Tag
		}
	}
	read := 0
	for _, area := range areas {
		if stop() {
			return hits, false, true
		}
		err := e.MessageMgr.EachMessage(area.ID, func(m *message.DisplayMessage) bool {
			read++
			if read%msgSearchCheckEvery == 0 && stop() {
				stopped = true
				return true // the walk ends on it below
			}
			return header(m)
		}, func(m *message.DisplayMessage) bool {
			if stopped {
				return false
			}
			if !visible(m) || !q.Match(m.Subject, m.Body) {
				return true
			}
			hits = append(hits, msgSearchHit{
				areaID:   area.ID,
				confTag:  confTags[area.ConferenceID],
				areaTag:  area.Tag,
				msgNum:   m.MsgNum,
				subject:  m.Subject,
				from:     m.From,
				dateTime: m.DateTime,
			})
			if len(hits) >= msgSearchMaxResults {
				full = true
				return false
			}
			return true
		})
		if err != nil && !errors.Is(err, message.ErrAreaNotFound) {
			slog.Warn("message search: skipping area", "area", area.Tag, "error", err)
		}
		if stopped || full {
			return hits, full, stopped
		}
	}
	return hits, false, false
}

// msgSearchStopKey reports whether a running search should stop: the
// caller has pressed Esc or Ctrl-C, or has gone (the connection closed or the
// session ended), without waiting for a key. Anything else the caller has
// typed is left to be read, so typing ahead (the number of a result, or an
// arrow or PgDn, which start with an Esc) still works.
func msgSearchStopKey(ih *editor.InputHandler) bool {
	b, ok, err := ih.PendingByte(0)
	switch {
	case err != nil:
		return true
	case !ok:
		return false
	case b == 3:
		_, _ = ih.ReadKey()
		return true
	case b != editor.KeyEsc:
		return false
	}
	// An Esc followed by [ or O is the start of a key's escape sequence,
	// waited for as ReadKey does: give the Esc back for the results prompt.
	if _, err := ih.ReadByteUntil(nil); err != nil {
		return true
	}
	next, ok, err := ih.PendingByte(ih.EscTimeout())
	if err == nil && ok && (next == '[' || next == 'O') {
		ih.ReturnInput([]byte{editor.KeyEsc})
		return false
	}
	return true
}

// msgSearchReadEnd is what SEARCHMSGS returns when reading the caller's
// answer to one of its prompts fails with err: Esc leaves quietly, a
// disconnect or idle timeout logs off, and anything else is passed up.
func msgSearchReadEnd(u *user.User, err error) (*user.User, string, error) {
	if errors.Is(err, errInputAborted) {
		return u, "", nil
	}
	if errors.Is(err, io.EOF) || errors.Is(err, editor.ErrIdleTimeout) {
		return nil, "LOGOFF", err
	}
	return u, "", err
}

// runSearchMsgs is the SEARCHMSGS runnable. args is the scope: blank or G
// for the current conference, C for the current area, A for every
// conference.
func runSearchMsgs(c *cmdCtx, args string) (*user.User, string, error) {
	e, terminal, outputMode, u := c.e, c.terminal, c.outputMode, c.currentUser
	str := e.Strings()
	if u == nil {
		e.writeString(terminal, outputMode, str.ScanLoginRequired)
		uiPause(time.Second)
		return nil, "", nil
	}
	scope := parseMsgSearchScope(args)
	if scope == msgSearchArea && u.CurrentMessageAreaID <= 0 {
		e.showScanNotice(terminal, outputMode, str.ScanNoAreaSelected)
		return u, "", nil
	}
	areas := msgSearchAreas(c, scope)
	if len(areas) == 0 {
		e.showScanNotice(terminal, outputMode, str.MsgSearchNoAreas)
		return u, "", nil
	}

	ih := getSessionIH(c.s)
	e.writeString(terminal, outputMode, str.MsgSearchPrompt)
	input, err := readScanLine(ih, terminal, outputMode, msgsearch.MaxQueryLen)
	if err != nil {
		return msgSearchReadEnd(u, err)
	}
	if strings.TrimSpace(input) == "" {
		return u, "", nil
	}
	q, err := msgsearch.Parse(input)
	if err != nil {
		e.writeStringf(terminal, outputMode, str.MsgSearchInvalid, callerText(err.Error()))
		uiPause(2 * time.Second)
		return u, "", nil
	}

	e.writeString(terminal, outputMode, str.MsgSearchSearching)
	header, visible := msgSearchFilters(c)
	hits, full, stopped := searchMessages(e, areas, q, header, visible, func() bool { return msgSearchStopKey(ih) })
	slog.Debug("message search", "node", c.nodeNumber, "query", q.String(), "areas", len(areas),
		"hits", len(hits), "full", full, "stopped", stopped)
	if len(hits) == 0 {
		if stopped {
			e.showScanNotice(terminal, outputMode, str.MsgSearchStopped)
		} else {
			e.showScanNotice(terminal, outputMode, str.MsgSearchNoMatches)
		}
		return u, "", nil
	}
	return runMsgSearchResults(c, q, hits, full, stopped)
}

// msgSearchPageSize is how many results fit on one page of a screen with
// height rows: the rest hold the header, column titles and the prompt.
func msgSearchPageSize(height int) int {
	if height <= 0 {
		height = 24
	}
	if n := height - 7; n >= 3 {
		return n
	}
	return 3
}

// runMsgSearchResults shows hits a page at a time. The caller types a
// result's number to read it, Enter or N for the next page (the last page
// stays put), P for the previous one, and Q or Esc to leave. full is set
// when the search hit msgSearchMaxResults and stopped when the caller ended
// it early, so a line under the header says the list is incomplete and why.
func runMsgSearchResults(c *cmdCtx, q *msgsearch.Query, hits []msgSearchHit, full, stopped bool) (*user.User, string, error) {
	e, terminal, outputMode, u := c.e, c.terminal, c.outputMode, c.currentUser
	str := e.Strings()
	ih := getSessionIH(c.s)
	height := c.termHeight
	if height <= 0 {
		height = u.ScreenHeight
	}
	perPage := msgSearchPageSize(height)
	pages := (len(hits) + perPage - 1) / perPage
	page := 0
	for {
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		e.writeStringf(terminal, outputMode, str.MsgSearchHeader, callerText(q.String()), len(hits))
		switch {
		case full:
			e.writeStringf(terminal, outputMode, str.MsgSearchPartial, len(hits))
		case stopped:
			e.writeStringf(terminal, outputMode, str.MsgSearchStoppedPartial, len(hits))
		}
		e.writeString(terminal, outputMode, str.MsgSearchColumns)
		start := page * perPage
		end := min(start+perPage, len(hits))
		for i := start; i < end; i++ {
			writeMsgSearchRow(terminal, outputMode, i+1, hits[i])
		}
		e.writeStringf(terminal, outputMode, str.MsgSearchPagePrompt, page+1, pages)
		input, err := readScanLine(ih, terminal, outputMode, 4)
		if err != nil {
			return msgSearchReadEnd(u, err)
		}
		switch strings.ToUpper(input) {
		case "Q":
			return u, "", nil
		case "", "N":
			if page+1 < pages {
				page++
			}
			continue
		case "P":
			if page > 0 {
				page--
			}
			continue
		}
		n := 0
		if _, scanErr := fmt.Sscanf(input, "%d", &n); scanErr != nil || n < 1 || n > len(hits) {
			continue
		}
		action, err := openMsgSearchHit(c, q, hits[n-1])
		if err != nil || action == "LOGOFF" {
			return nil, "LOGOFF", err
		}
	}
}

// Column widths of a results row, which fits 79 columns.
const (
	msgSearchNumWidth     = 4
	msgSearchAreaWidth    = 20
	msgSearchMsgWidth     = 5
	msgSearchSubjectWidth = 24
	msgSearchFromWidth    = 14
)

// writeMsgSearchRow writes result n: its conference and area, message
// number, subject, sender and date. The subject and sender come from the
// message, so as in the message list they are made inert and their pipes
// doubled after fitting: a colour code in a subject prints as typed and
// takes the columns it shows in, instead of recolouring and shifting the
// rest of the row.
func writeMsgSearchRow(terminal *term.Terminal, outputMode ansi.OutputMode, n int, h msgSearchHit) {
	where := h.areaTag
	if h.confTag != "" {
		where = h.confTag + "/" + h.areaTag
	}
	fit := func(s string, w int) string {
		return ansi.PadRight(ansi.TruncateRunes(s, w, ""), w)
	}
	msgCell := func(s string, w int) string {
		return threadEscapePipes(fit(threadPlainText(s), w))
	}
	line := fmt.Sprintf("\r\n|15%*d |09%s |07%*d |15%s |11%s |08%s",
		msgSearchNumWidth-1, n,
		callerText(fit(where, msgSearchAreaWidth)),
		msgSearchMsgWidth, h.msgNum,
		msgCell(h.subject, msgSearchSubjectWidth),
		msgCell(h.from, msgSearchFromWidth),
		boardTime(h.dateTime).Format("01/02/06"))
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)
}

// openMsgSearchHit opens h in the message reader, after checking again that
// the caller may read its area and that the message is still there and
// visible to them. Next and previous (N, S) in the reader move only between
// the area's messages q matches; the thread pane and menu still show the
// whole thread, as the caller may see it. L or Q comes back to the list. The
// caller's current area and the area's last-read pointer are put back when
// the reader returns.
func openMsgSearchHit(c *cmdCtx, q *msgsearch.Query, h msgSearchHit) (string, error) {
	e, terminal, outputMode, u := c.e, c.terminal, c.outputMode, c.currentUser
	area, ok := e.MessageMgr.GetAreaByID(h.areaID)
	if !ok || !msgSearchAllowed(c, area) {
		e.showScanNotice(terminal, outputMode, e.Strings().MsgSearchGone)
		return "", nil
	}
	m, err := e.MessageMgr.GetMessage(h.areaID, h.msgNum)
	_, visible := msgSearchFilters(c)
	if err != nil || !visible(m) {
		e.showScanNotice(terminal, outputMode, e.Strings().MsgSearchGone)
		return "", nil
	}
	total, err := e.MessageMgr.GetMessageCountForArea(h.areaID)
	if err != nil || total < h.msgNum {
		total = h.msgNum
	}

	origAreaID, origAreaTag := u.CurrentMessageAreaID, u.CurrentMessageAreaTag
	origConfID, origConfTag := u.CurrentMsgConferenceID, u.CurrentMsgConferenceTag
	lastRead, lrErr := e.MessageMgr.GetLastRead(h.areaID, u.Handle)
	defer func() {
		if lrErr == nil {
			if err := e.MessageMgr.SetLastRead(h.areaID, u.Handle, lastRead); err != nil {
				slog.Error("message search: failed to restore lastread", "node", c.nodeNumber, "area", h.areaID, "error", err)
			}
		}
		u.CurrentMessageAreaID, u.CurrentMessageAreaTag = origAreaID, origAreaTag
		u.CurrentMsgConferenceID, u.CurrentMsgConferenceTag = origConfID, origConfTag
		if c.userManager != nil {
			if err := c.userManager.UpdateUser(u); err != nil {
				slog.Error("message search: failed to restore user area", "node", c.nodeNumber, "error", err)
			}
		}
	}()
	u.CurrentMessageAreaID, u.CurrentMessageAreaTag = area.ID, area.Tag
	e.setUserMsgConference(u, area.ConferenceID)

	width, height := u.ScreenWidth, u.ScreenHeight
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	matches := func(m *message.DisplayMessage) bool { return q.Match(m.Subject, m.Body) }
	_, action, _, err := runMessageReaderOpts(e, c.s, terminal, c.userManager, u, c.nodeNumber,
		c.sessionStartTime, outputMode, h.msgNum, total, false, width, height, nil,
		readerOpts{fromList: true, navFilter: matches})
	return action, err
}

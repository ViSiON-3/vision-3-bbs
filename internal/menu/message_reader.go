package menu

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// Default message header style if user hasn't selected one. Style 1 is the
// clean "ViSiON/3 Message Header"; the previous default (5) shipped with
// alignment glitches ("Sent To" and an empty, misplaced "Total Msg"). See #301.
const defaultMsgHdrStyle = 1

// Message reader navigation options (Pascal's 10-option bar)
var msgReaderOptions = []MsgLightbarOption{
	{Label: " Next ", HotKey: 'N'},
	{Label: " Reply ", HotKey: 'R'},
	{Label: " Prev ", HotKey: 'S'},
	{Label: " Thread ", HotKey: 'T'},
	{Label: " Post ", HotKey: 'P'},
	{Label: " Jump ", HotKey: 'J'},
	{Label: " Mail ", HotKey: 'M'},
	{Label: " List ", HotKey: 'L'},
	{Label: " Quit ", HotKey: 'Q'},
}

// msgReaderIgnoreOption adds the current author to the caller's Twit List. It
// sits before Quit and is left out while the sysop has Twit Lists turned off.
var msgReaderIgnoreOption = MsgLightbarOption{Label: " Twit ", HotKey: 'I'}

// readerBarSuffix returns suffix, or nothing when the bar and suffix together
// would reach the last column of a termWidth-column row (the bar is bounded
// by a column on each side). Like the separator line above it, the bar stays
// off the last column, which can scroll the screen on some terminals.
func readerBarSuffix(options []MsgLightbarOption, suffix string, termWidth int) string {
	width := 2 + ansi.VisibleLength(string(ansi.ReplacePipeCodes([]byte(suffix))))
	for _, opt := range options {
		width += len(opt.Label)
	}
	if termWidth > 0 && width >= termWidth {
		return ""
	}
	return suffix
}

// msgReaderInactiveColor greys out a bar item that does nothing on the
// current message (Thread, on a message with no thread to show).
const msgReaderInactiveColor = 8

// msgReaderDeleteOption is appended to the lightbar for sysop/co-sysop users only.
// LoColor 4 (dark red) distinguishes it from regular options.
var msgReaderDeleteOption = MsgLightbarOption{Label: " Del ", HotKey: 'D', LoColor: 4}

// msgOwnershipFilter reports whether a message may be shown to the current user.
// A nil filter means no extra filtering; a non-nil filter is used for areas like
// PRIVMAIL where a user may only see their own messages, and for newscan
// To/From/date/range searches (ScanConfig.filter), regardless of how navigation
// arrives at a message number. The reader and message list always add the
// private-mail rule on top (see withPrivacy), so a nil filter never exposes
// someone else's private message.
type msgOwnershipFilter func(*message.DisplayMessage) bool

// handleDirectory tells whether a handle belongs to an account (deleted or
// not). *user.UserMgr implements it.
type handleDirectory interface {
	HandleExists(handle string) bool
}

// privateMailReader is who is reading, for withPrivacy: the user, the access
// level at which a user is a sysop (ServerConfig.SysOpLevel), and the account
// directory that decides whether a private message is deliverable.
type privateMailReader struct {
	user       *user.User
	sysOpLevel int
	handles    handleDirectory
}

// privateMailReaderFor describes u reading through e, with um as the account
// directory. um may be nil (no undeliverable-mail allowance).
func privateMailReaderFor(e *MenuExecutor, um *user.UserMgr, u *user.User) privateMailReader {
	r := privateMailReader{user: u}
	if e != nil {
		r.sysOpLevel = e.GetServerConfig().SysOpLevel
	}
	if um != nil {
		r.handles = um
	}
	return r
}

// withPrivacy returns f narrowed so that private messages are only accepted
// when the reader may see them. f may be nil. The rule, for the message
// reader, the message list and newscan:
//
//   - A private message is visible to the account whose handle is its To or
//     From (message.DisplayMessage.VisibleTo). This is the only rule for
//     every other user and for all mail that reached an account.
//   - A private message is "undeliverable" when its To is not the handle of
//     any account, deleted or not (user.UserMgr.HandleExists): typically
//     netmail or QWK mail addressed to a real name that could not be resolved
//     to one user when it was imported (see user.UserMgr.ResolveRecipient and
//     'v3mail readdress'). A sysop (access level at or above SysOpLevel) may
//     read undeliverable mail too, so it is not lost to everyone.
//
// The allowance lives here rather than in VisibleTo because it depends on the
// account directory and on board configuration; QWK export and the script API
// stay handle-only. A nil user sees no private mail. Without a directory, or
// with one that does not know the reader's own handle, no mail counts as
// undeliverable, so a missing directory never widens access.
func withPrivacy(r privateMailReader, f msgOwnershipFilter) msgOwnershipFilter {
	var handle string
	sysop := false
	if r.user != nil {
		handle = r.user.Handle
		sysop = r.sysOpLevel > 0 && r.user.AccessLevel >= r.sysOpLevel &&
			r.handles != nil && r.handles.HandleExists(handle)
	}
	return func(m *message.DisplayMessage) bool {
		undeliverable := sysop && !r.handles.HandleExists(m.To)
		if !m.VisibleTo(handle) && !undeliverable {
			return false
		}
		return f == nil || f(m)
	}
}

// runMessageReader is the core message reading loop matching Pascal's Scanboard + Readcurbul.
// It displays messages using MSGHDR.<n> templates with DataFile substitution,
// shows a 10-option lightbar for navigation, and handles single-key input.
// Messages msgFilter rejects, and other users' private messages, are skipped (in
// the direction of the last navigation command) and never rendered.
func runMessageReader(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, outputMode ansi.OutputMode,
	startMsg int, totalMsgCount int, isNewScan bool,
	termWidth int, termHeight int, msgFilter msgOwnershipFilter, counters ...*twitScanCounter) (*user.User, string, error) {
	u, action, _, err := runMessageReaderOpts(e, s, terminal, userManager, currentUser, nodeNumber,
		sessionStartTime, outputMode, startMsg, totalMsgCount, isNewScan, termWidth, termHeight, msgFilter, readerOpts{}, counters...)
	return u, action, err
}

// readerOpts is how the reader was opened.
type readerOpts struct {
	// fromList is set when the message list opened the reader. L then
	// returns "LIST" instead of opening a second list over the first, and
	// the list re-selects the message.
	fromList bool
}

// runMessageReaderOpts is runMessageReader with readerOpts. The extra result
// is the message the reader was on when it returned, for a list to put its
// cursor back on.
func runMessageReaderOpts(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, outputMode ansi.OutputMode,
	startMsg int, totalMsgCount int, isNewScan bool,
	termWidth int, termHeight int, msgFilter msgOwnershipFilter, opts readerOpts, counters ...*twitScanCounter) (*user.User, string, int, error) {

	baseFilter := withPrivacy(privateMailReaderFor(e, userManager, currentUser), msgFilter)
	matcher := twitMatcher(e, userManager, currentUser)
	quoteBanners := newQuoteBannerMatcher(e.Strings())
	var counter *twitScanCounter
	if len(counters) > 0 {
		counter = counters[0]
	}
	msgFilter = withTwit(func() *user.TwitMatcher { return matcher }, baseFilter, counter)
	// The thread keys, signposts and pane see the thread through the same
	// rules as navigation, but a hidden post they pass over is not one the
	// scan skipped, so it does not count toward a newscan's twit tally.
	threadFilter := withTwit(func() *user.TwitMatcher { return matcher }, baseFilter, nil)
	glyphs := threadGlyphsFor(outputMode)
	currentAreaID := currentUser.CurrentMessageAreaID
	currentAreaTag := currentUser.CurrentMessageAreaTag

	// Get conference and area names for display
	confName := "Local"
	areaName := currentAreaTag
	if currentUser.CurrentMsgConferenceID != 0 && e.ConferenceMgr != nil {
		if conf, found := e.ConferenceMgr.GetByID(currentUser.CurrentMsgConferenceID); found {
			confName = conf.Name
		}
	}
	if area, found := e.MessageMgr.GetAreaByID(currentAreaID); found {
		areaName = area.Name
	}

	// Determine message header style
	hdrStyle := currentUser.MsgHdr
	if hdrStyle < 1 {
		hdrStyle = defaultMsgHdrStyle
	}
	// No upper bound: a style numbered above any fixed ceiling was silently
	// redrawn as the default, so a caller who picked it saw a different header
	// than the one they chose. If the template turns out not to exist, the
	// load below already falls back.

	// Load the MSGHDR template file
	hdrTemplatePath := e.menuFile("templates", "message_headers", fmt.Sprintf("MSGHDR.%d.ans", hdrStyle))
	hdrTemplateBytes, hdrErr := ansi.GetAnsiFileContent(hdrTemplatePath)
	if hdrErr != nil {
		slog.Error("failed to load message header", "node", nodeNumber, "file", fmt.Sprintf("MSGHDR.%d.ans", hdrStyle), "error", hdrErr)
		// Fallback to style 2 (simple text format)
		hdrTemplatePath = e.menuFile("templates", "message_headers", "MSGHDR.2.ans")
		hdrTemplateBytes, hdrErr = ansi.GetAnsiFileContent(hdrTemplatePath)
		if hdrErr != nil {
			e.writeString(terminal, outputMode, e.Strings().MsgHdrLoadError)
			uiPause(1 * time.Second)
			return nil, "", startMsg, fmt.Errorf("failed loading MSGHDR templates")
		}
	}

	// Trim trailing empty lines from header template to prevent scrolling.
	// ANSI templates are often padded to 24/25 rows, but the message reader
	// handles body/footer positioning separately, so trailing blank lines
	// just push header content off the top of the screen.
	hdrTemplateBytes = bytes.TrimRight(hdrTemplateBytes, "\r\n ")

	// Terminal dimensions are now passed as parameters to use user's adjusted preferences
	// Default to 80x24 if not provided
	if termWidth <= 0 {
		termWidth = 80
	}
	if termHeight <= 0 {
		termHeight = 24
	}

	// Use the session-scoped InputHandler so this reader loop, any editor
	// invocations, and the lightbar menus all share a single goroutine reading
	// from the SSH session — no keystrokes are lost when control transfers.
	sessionIH := getSessionIH(s)
	reader := bufio.NewReader(sessionIH)

	// Lightbar colors from theme
	hiColor := e.Theme().YesNoHighlightColor
	loColor := 9 // Bright blue unselected items
	boundsColor := 1

	// Build option set: append Delete option for sysop/co-sysop users
	cfg := e.GetServerConfig()
	isSysop := currentUser.AccessLevel >= cfg.CoSysOpLevel
	twitOn := twitListsOn(e)
	activeOptions := make([]MsgLightbarOption, 0, len(msgReaderOptions)+2)
	for _, opt := range msgReaderOptions {
		if opt.HotKey == 'Q' && twitOn {
			activeOptions = append(activeOptions, msgReaderIgnoreOption)
		}
		activeOptions = append(activeOptions, opt)
	}
	if isSysop {
		activeOptions = append(activeOptions, msgReaderDeleteOption)
	}
	barSuffix := e.Strings().MsgReadingSuffix
	if isNewScan {
		barSuffix = e.Strings().MsgNewScanSuffix
	}
	barSuffix = readerBarSuffix(activeOptions, barSuffix, termWidth)

	currentMsgNum := startMsg
	quitNewscan := false
	// navDir is the direction (+1/-1) of the last navigation command; it steers
	// the msgFilter skip so "previous" keeps moving backward past hidden messages.
	navDir := 1

	// Next/prev navigation must move between visible messages only (msgFilter
	// is always set: at minimum it hides other users' private mail). These
	// helpers locate the adjacent visible message so the reader can show the
	// usual first/last prompt at the edges instead of falling off the end and
	// dropping back to the menu.
	msgVisible := func(n int) bool {
		if n < 1 || n > totalMsgCount {
			return false
		}
		m, err := e.MessageMgr.GetMessage(currentAreaID, n)
		if err != nil || m.IsDeleted {
			return false
		}
		return msgFilter == nil || msgFilter(m)
	}
	nextVisible := func(from int) int {
		for n := from + 1; n <= totalMsgCount; n++ {
			if msgVisible(n) {
				return n
			}
		}
		return 0
	}
	prevVisible := func(from int) int {
		for n := from - 1; n >= 1; n-- {
			if msgVisible(n) {
				return n
			}
		}
		return 0
	}

readerLoop:
	for {
		if currentMsgNum > totalMsgCount || currentMsgNum < 1 {
			break readerLoop
		}

		// Load current message
		currentMsg, msgErr := e.MessageMgr.GetMessage(currentAreaID, currentMsgNum)
		if msgErr != nil {
			slog.Error("failed to read message", "node", nodeNumber, "msg", currentMsgNum, "area", currentAreaID, "error", msgErr)
			// Try next message
			currentMsgNum++
			continue
		}

		// Skip deleted messages
		if currentMsg.IsDeleted {
			currentMsgNum++
			continue
		}

		// Ownership boundary (e.g. PRIVMAIL): never render a message the filter
		// rejects, regardless of how navigation reached this number. Skip in the
		// direction of the last navigation command so prev/next stay intuitive.
		if msgFilter != nil && !msgFilter(currentMsg) {
			currentMsgNum += navDir
			continue
		}

		// The reply thread this message sits in, as this caller may see it.
		tv := readerThreadView(e, currentAreaID, currentMsgNum, currentMsg, threadFilter)

		// Build Pascal-style substitution map
		templateUsesUserNote := headerTemplateUsesUserNote(hdrTemplateBytes)
		replyCount := 0
		if e.MessageMgr != nil {
			if count, err := e.MessageMgr.GetThreadReplyCount(currentAreaID, currentMsg.MsgNum, currentMsg.Subject); err != nil {
				slog.Warn("failed to get reply count", "area", currentAreaID, "msg", currentMsg.MsgNum, "error", err)
			} else {
				replyCount = count
			}
		}
		nodelists := e.Nodelists
		if !headerTemplateUsesSystemNames(hdrTemplateBytes) {
			nodelists = nil // no @Y@ or @R@ to fill
		}
		substitutions := buildMsgSubstitutions(currentMsg, currentAreaTag, currentMsgNum, totalMsgCount, !templateUsesUserNote, replyCount, confName, areaName, e.MessageMgr, currentAreaID, userManager, nodeNumber, e.V3NetStatus, nodelists)
		// @H@: direct replies to this message in the reply tree, as the
		// pane counts them. @E@ keeps its subject-based count.
		substitutions['H'] = strconv.Itoa(tv.directReplies(currentMsgNum))
		// For CP437 terminals, convert UTF-8 substitution values to CP437 bytes so that
		// multi-byte runes (e.g. Cyrillic from FTN UTF-8 messages) render as '?' instead
		// of raw UTF-8 bytes that display as CP437 box-drawing characters.
		if outputMode == ansi.OutputModeCP437 {
			substitutions = convertSubsToCP437(substitutions)
		}
		// Header templates are 80-column art, so gap fills stop at the art's
		// width rather than stretching across a wider terminal.
		autoWidths := buildAutoWidths(substitutions, totalMsgCount, min(termWidth, ansi.ArtWidth), outputMode == ansi.OutputModeCP437)

		// Process template with substitutions (auto-detects @CODE@ or |X format)
		processedHeader := processTemplate(hdrTemplateBytes, substitutions, autoWidths, outputMode == ansi.OutputModeCP437)
		processedHeader = fitArt(terminal, processedHeader, termWidth, outputMode == ansi.OutputModeUTF8)

		// Process message body and pre-format all lines
		area, _ := e.MessageMgr.GetAreaByID(currentAreaID)
		includeOrigin := area != nil && (area.AreaType == "echomail" || area.AreaType == "netmail") &&
			currentMsg.OrigAddr != "" && !hasOriginLine(currentMsg.Body)
		formattedBody := formatMessageBody(currentMsg.Body, currentMsg.OrigAddr, includeOrigin, quoteBanners)

		// Convert colour pipe codes to ANSI sequences. Only colour: the body
		// was written on another system, so "||" and the screen-control codes
		// are text, not ViSiON/3 escapes.
		processedBodyBytes := ansi.ReplaceColorPipeCodes([]byte(formattedBody))
		wrappedBodyLines := messageBodyLines(string(processedBodyBytes), currentMsg.Charset, termWidth, outputMode)

		// Calculate available body height
		// Find the actual bottom row of the header using ANSI cursor tracking
		// Measured with every wrap made explicit, since findHeaderEndRow only
		// counts line feeds and a template drawn without them would otherwise
		// measure as a single row.
		headerEndRow := findHeaderEndRow(ansi.HardWrap(processedHeader, ansi.ArtWidth, outputMode == ansi.OutputModeUTF8))
		bodyStartRow := headerEndRow + 1 // Start body on next row after header
		barLines := 2                    // Horizontal line + lightbar
		bodyAvailHeight := termHeight - bodyStartRow - barLines
		if bodyAvailHeight < 1 {
			bodyAvailHeight = 5
		}

		// The docked thread pane sits under the header and moves the body
		// down while it is open (T opens and closes it). A message that is
		// not in a thread has no pane and no signposts: the separator row
		// is a plain rule and the body keeps the rows. A reply whose parent
		// is deleted or hidden is still in one, even with nothing else to
		// show: it keeps its signposts and keys, though there is no pane.
		paneTop, fullBodyStart, fullBodyAvail := bodyStartRow, bodyStartRow, bodyAvailHeight
		var paneLines []string
		layoutPane := func() {
			paneLines = nil
			bodyStartRow, bodyAvailHeight = fullBodyStart, fullBodyAvail
			if !tv.hasThread() || !sessionThreadPaneOpen(s, currentUser) {
				return
			}
			// A short thread takes only the rows it fills.
			if rows := min(paneRowsFor(bodyAvailHeight), len(tv.rows)); rows > 0 {
				paneLines = renderThreadPane(tv, rows, termWidth-1, e.Strings().MsgThreadPaneTitle, glyphs, outputMode)
				bodyStartRow += rows + 2
				bodyAvailHeight -= rows + 2
			}
		}
		layoutPane()
		signposts := "|08" + strings.Repeat("─", termWidth-1) + "|07"
		inThread := tv.hasThread() || tv.isReply()
		if inThread {
			signposts = renderSignposts(readerSignposts(tv, glyphs), termWidth-1, glyphs, outputMode)
		}
		// The bar's Thread item is lit only when there is a pane to open.
		for i := range activeOptions {
			if activeOptions[i].HotKey == 'T' {
				activeOptions[i].LoColor = 0
				if !tv.hasThread() {
					activeOptions[i].LoColor = msgReaderInactiveColor
				}
			}
		}

		// Initialize scroll state for this message
		scrollOffset := 0
		totalBodyLines := len(wrappedBodyLines)

		// The colour a body line is drawn in can be set by a line above it, so
		// painting a window that starts part-way down the body has to restore
		// the state that window inherits. bodyEntryState[i] is the escape that
		// puts the terminal into the state line i begins in, which is what the
		// line would have inherited had the body been drawn from the top.
		bodyEntryState := buildBodyEntryStates(wrappedBodyLines)
		needsRedraw := true
		needsBodyRedraw := false

		drawBody := func() {
			// Display visible portion of message body using explicit cursor positioning
			for i := 0; i < bodyAvailHeight; i++ {
				lineNum := bodyStartRow + i
				// Position cursor at specific line
				terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(lineNum, 1)), outputMode)
				// Reset before clearing: erase-in-line paints with the current
				// background, so without this a line that set one would smear
				// it across the rest of the row.
				terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m\x1b[K"), outputMode)
				// Display line if available
				lineIdx := scrollOffset + i
				if lineIdx < totalBodyLines {
					// Restore the colour this line inherits. For the body's own
					// first line that is grey, which keeps the cyan of the |11
					// reading suffix out of plain body text.
					terminalio.WriteProcessedBytes(terminal, []byte(bodyEntryState[lineIdx]), outputMode)
					terminalio.WriteProcessedBytes(terminal, []byte(wrappedBodyLines[lineIdx]), outputMode)
				}
			}
		}

		// Update lastread when first displaying message. Advance only:
		// paging back to an older message must not mark newer ones unread
		// again, or the new-mail and newscan counts report them twice.
		if lrErr := e.MessageMgr.AdvanceLastRead(currentAreaID, currentUser.Handle, currentMsgNum); lrErr != nil {
			slog.Error("failed to update last read", "node", nodeNumber, "error", lrErr)
		}

		// readerScrollKeys end the interactive lightbar and are handed back
		// here as pendingKey, so scrolling works whether or not the bar has
		// been entered with Left/Right.
		readerScrollKeys := []int{
			editor.KeyArrowUp, editor.KeyArrowDown, editor.KeyPageUp, editor.KeyPageDown,
			editor.KeyCtrlE, editor.KeyCtrlX, editor.KeyCtrlR, editor.KeyCtrlC,
			// The thread keys are not on the bar, so the bar hands them
			// back too rather than swallowing them.
			'B', 'b', 'F', 'f', 'A', 'a', '[', ']',
		}
		pendingKey := 0

		// Inner loop for scrolling and command handling
	scrollLoop:
		for {
			var selectedKey rune // Declare here so it's available in all code paths

			// Redraw screen if needed
			if needsRedraw {
				// Clear screen and display header.
				// CP437 mode: write raw bytes — substitutions were pre-converted and the
				// ANSI art bytes must reach the terminal unchanged.
				// Other modes (UTF-8): use WriteProcessedBytes so CP437 art bytes in the
				// MSGHDR template are converted to Unicode before reaching the terminal.
				terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
				if outputMode == ansi.OutputModeCP437 {
					_, _ = terminal.Write(processedHeader) // best-effort display
				} else {
					terminalio.WriteProcessedBytes(terminal, processedHeader, outputMode)
				}

				for i, line := range paneLines {
					terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(paneTop+i, 1)), outputMode)
					terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)
				}

				drawBody()

				// Display footer: lightbar only
				suffixText := barSuffix

				// The separator row above the bar carries the thread
				// signposts: where B, F and A lead from this message.
				terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(termHeight-1, 1)), outputMode)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(signposts)), outputMode)

				// Position cursor at last row for lightbar
				terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(termHeight, 1)), outputMode)

				// Draw the lightbar menu
				drawMsgLightbarStatic(terminal, activeOptions, outputMode, hiColor, loColor, suffixText, 0, true, boundsColor)

				needsRedraw = false
				needsBodyRedraw = false
			} else if needsBodyRedraw {
				// Only redraw body area to avoid refreshing header/footer while scrolling
				drawBody()
				needsBodyRedraw = false
			}

			// Read key directly from the shared session input handler so arrow/page
			// keys are decoded consistently across reader/list/header lightbars.
			key := pendingKey
			pendingKey = 0
			if key == 0 {
				var keyErr error
				key, keyErr = sessionIH.ReadKey()
				if keyErr != nil {
					if errors.Is(keyErr, editor.ErrIdleTimeout) {
						return nil, "LOGOFF", currentMsgNum, editor.ErrIdleTimeout
					}
					if errors.Is(keyErr, io.EOF) {
						return nil, "LOGOFF", currentMsgNum, io.EOF
					}
					continue
				}
			}

			// Handle scrolling keys first
			switch key {
			case editor.KeyEsc: // ESC key - quit reader
				quitNewscan = true
				break readerLoop

			case editor.KeyArrowUp, editor.KeyCtrlE: // Up arrow - scroll up one line
				if scrollOffset > 0 {
					scrollOffset--
					needsBodyRedraw = true
				}
				continue

			case editor.KeyArrowDown, editor.KeyCtrlX: // Down arrow - scroll down one line
				if totalBodyLines > bodyAvailHeight && scrollOffset < totalBodyLines-bodyAvailHeight {
					scrollOffset++
					needsBodyRedraw = true
				}
				continue

			case editor.KeyPageUp, editor.KeyCtrlR: // Page Up
				pageSize := bodyAvailHeight - 2
				if pageSize < 5 {
					pageSize = 5
				}
				scrollOffset -= pageSize
				if scrollOffset < 0 {
					scrollOffset = 0
				}
				needsBodyRedraw = true
				continue

			case editor.KeyPageDown, editor.KeyCtrlC: // Page Down
				pageSize := bodyAvailHeight - 2
				if pageSize < 5 {
					pageSize = 5
				}
				scrollOffset += pageSize
				maxScroll := totalBodyLines - bodyAvailHeight
				if maxScroll < 0 {
					maxScroll = 0
				}
				if scrollOffset > maxScroll {
					scrollOffset = maxScroll
				}
				needsBodyRedraw = true
				continue

			case editor.KeyArrowLeft, editor.KeyArrowRight, editor.KeyCtrlS, editor.KeyCtrlD: // Left/Right arrow - activate interactive lightbar
				suffixText := barSuffix

				terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(termHeight, 1)), outputMode)

				// Determine initial direction based on which arrow was pressed
				initialDir := 0
				switch key {
				case editor.KeyArrowLeft, editor.KeyCtrlS:
					initialDir = -1 // Left arrow
				case editor.KeyArrowRight, editor.KeyCtrlD:
					initialDir = 1 // Right arrow
				}

				selKey, passKey, lbErr := runMsgLightbar(sessionIH, terminal, activeOptions, outputMode, hiColor, loColor, suffixText, initialDir, true, boundsColor, readerScrollKeys)
				if lbErr != nil {
					if errors.Is(lbErr, editor.ErrIdleTimeout) {
						return nil, "LOGOFF", currentMsgNum, editor.ErrIdleTimeout
					}
					if errors.Is(lbErr, io.EOF) {
						return nil, "LOGOFF", currentMsgNum, io.EOF
					}
					slog.Error("lightbar error", "node", nodeNumber, "error", lbErr)
					break readerLoop
				}
				if passKey != 0 {
					pendingKey = passKey // scroll key pressed in the bar
					continue
				}
				selectedKey = rune(selKey)
				// Don't continue here - fall through to handle the selected command
			}

			if selectedKey == 0 {
				// If not a scrolling key, show the lightbar for command selection
				// First handle simple single-key commands that bypass the lightbar
				if key >= 32 && key <= 126 {
					singleKey := rune(key)
					// Check if it's a direct command key
					switch unicode.ToUpper(singleKey) {
					case 'N', 'R', 'S', 'T', 'P', 'J', 'M', 'L', 'Q', 'D', 'K', 'I', '?',
						'B', 'F', 'A', '[', ']':
						selectedKey = unicode.ToUpper(singleKey)
					default:
						// Not a recognized command, show lightbar
						suffixText := barSuffix

						// Position cursor at last row for lightbar
						terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(termHeight, 1)), outputMode)

						selKey, passKey, lbErr := runMsgLightbar(sessionIH, terminal, activeOptions, outputMode, hiColor, loColor, suffixText, 0, true, boundsColor, readerScrollKeys)
						if lbErr != nil {
							if errors.Is(lbErr, editor.ErrIdleTimeout) {
								return nil, "LOGOFF", currentMsgNum, editor.ErrIdleTimeout
							}
							if errors.Is(lbErr, io.EOF) {
								return nil, "LOGOFF", currentMsgNum, io.EOF
							}
							slog.Error("lightbar error", "node", nodeNumber, "error", lbErr)
							break readerLoop
						}
						if passKey != 0 {
							pendingKey = passKey // scroll key pressed in the bar
							continue
						}
						selectedKey = rune(selKey)
					}
				} else if key == editor.KeyEnter {
					selectedKey = 'N' // Enter = Next
				} else if key == editor.KeyEsc {
					selectedKey = 'Q' // ESC = Quit
				} else if key < 32 {
					// Stray control byte (e.g. NUL/LF trailing a telnet CR) -
					// ignore it rather than dropping into the modal lightbar.
					continue
				} else {
					// Multi-byte sequence that wasn't handled as scrolling - show lightbar
					suffixText := barSuffix

					terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(termHeight, 1)), outputMode)

					selKey, passKey, lbErr := runMsgLightbar(sessionIH, terminal, activeOptions, outputMode, hiColor, loColor, suffixText, 0, true, boundsColor, readerScrollKeys)
					if lbErr != nil {
						if errors.Is(lbErr, editor.ErrIdleTimeout) {
							return nil, "LOGOFF", currentMsgNum, editor.ErrIdleTimeout
						}
						if errors.Is(lbErr, io.EOF) {
							return nil, "LOGOFF", currentMsgNum, io.EOF
						}
						slog.Error("lightbar error", "node", nodeNumber, "error", lbErr)
						break readerLoop
					}
					if passKey != 0 {
						pendingKey = passKey // scroll key pressed in the bar
						continue
					}
					selectedKey = rune(selKey)
				}
			}

			// Handle command from lightbar or direct key
			if selectedKey == 0 {
				continue
			}

			// Now handle message navigation commands
			// Handle the selected command
			switch selectedKey {
			case 'N': // Next message
				navDir = 1
				if msgFilter != nil {
					if nxt := nextVisible(currentMsgNum); nxt > 0 {
						currentMsgNum = nxt
						break scrollLoop
					}
					e.showReaderNotice(terminal, outputMode, e.Strings().MsgEndOfMessages, termHeight)
					break readerLoop
				}
				if currentMsgNum < totalMsgCount {
					currentMsgNum++
					break scrollLoop // Exit scroll loop to load next message
				} else {
					e.showReaderNotice(terminal, outputMode, e.Strings().MsgEndOfMessages, termHeight)
					break readerLoop
				}

			case 'R': // Reply
				replyResult := handleReply(e, s, sessionIH, terminal, userManager, currentUser, nodeNumber,
					outputMode, currentMsg, currentAreaID, &totalMsgCount, confName, areaName, termWidth, termHeight)
				if replyResult == "LOGOFF" {
					return nil, "LOGOFF", currentMsgNum, io.EOF
				}
				// Reload the message: the reply is now in its thread, so the
				// pane, signposts and reply count have to be rebuilt.
				continue readerLoop

			case 'P': // Post new message
				terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
				_, _, _ = runComposeMessageWithIH(e, s, sessionIH, terminal, userManager, currentUser, nodeNumber,
					sessionStartTime, "", outputMode, termWidth, termHeight)
				// Refresh total count
				newTotal, _ := e.MessageMgr.GetMessageCountForArea(currentAreaID)
				if newTotal > 0 {
					totalMsgCount = newTotal
				}
				// Redraw message after posting
				needsRedraw = true
				continue

			case 'S': // Prev - go back one message
				navDir = -1
				if msgFilter != nil {
					if prv := prevVisible(currentMsgNum); prv > 0 {
						currentMsgNum = prv
						break scrollLoop
					}
					// Already at the first visible message: stay put.
					e.showReaderNotice(terminal, outputMode, e.Strings().MsgFirstMessage, termHeight)
					needsRedraw = true
					continue
				}
				if currentMsgNum > 1 {
					currentMsgNum--
					break scrollLoop
				} else {
					e.showReaderNotice(terminal, outputMode, e.Strings().MsgFirstMessage, termHeight)
					needsRedraw = true
					continue
				}

			case 'I': // Ignore author (T remains Thread).
				if !twitOn {
					continue // Ignore if Twit Lists are turned off
				}
				if strings.EqualFold(strings.TrimSpace(currentMsg.From), "Anonymous") || strings.EqualFold(strings.TrimSpace(currentMsg.From), strings.TrimSpace(e.Strings().AnonymousName)) {
					e.showReaderNotice(terminal, outputMode, e.Strings().TwitAnonymous, termHeight)
					needsRedraw = true
					continue
				}
				name := currentMsg.From
				// Imported JAM records may also carry MsgLocal, so compare the
				// origin with this area's configured board address instead.
				localOrigin := strings.TrimSpace(currentMsg.OrigAddr) == ""
				if area, ok := e.MessageMgr.GetAreaByID(currentAreaID); ok && strings.TrimSpace(area.OriginAddr) != "" {
					localOrigin = localOrigin || strings.EqualFold(strings.TrimSpace(currentMsg.OrigAddr), strings.TrimSpace(area.OriginAddr))
				}
				if !localOrigin {
					name += "@" + currentMsg.OrigAddr
				} else if target, ok := userManager.ResolveRecipient(name); ok {
					// RealNameOnly posts still need stable IDs, self checks and exemptions.
					name = target.Handle
				}
				// Refuse yourself, staff, duplicates and a full list before
				// asking, so the caller is never asked to confirm an add
				// that cannot happen.
				if _, err := user.NewTwitEntry(currentUser, userManager, name, twitPolicy(e)); err != nil {
					e.showReaderNotice(terminal, outputMode, twitRefusal(e.Strings(), err), termHeight)
					needsRedraw = true
					continue
				}
				answer, confirmErr := promptSingleChar(reader, terminal, fmt.Sprintf(e.Strings().TwitQuickConfirm, strings.ReplaceAll(currentMsg.From, "|", "||")), outputMode)
				if confirmErr != nil {
					return currentUser, "LOGOFF", currentMsgNum, confirmErr
				}
				ctx := &cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, outputMode: outputMode}
				if answer == 'Y' && addTwit(ctx, name) {
					matcher = twitMatcher(e, userManager, currentUser)
					navDir = 1
					if nxt := nextVisible(currentMsgNum); nxt > 0 {
						currentMsgNum = nxt
						break scrollLoop
					}
					// The author was the last visible message: leave as N does.
					e.showReaderNotice(terminal, outputMode, e.Strings().MsgEndOfMessages, termHeight)
					break readerLoop
				}
				if answer == 'Y' {
					uiPause(readerNoticePause)
				}
				needsRedraw = true
				continue

			case 'T': // Open or close the thread pane
				if !tv.hasThread() {
					continue
				}
				setSessionThreadPaneOpen(s, !sessionThreadPaneOpen(s, currentUser))
				layoutPane()
				// Keep the top line where it was, as far as the new height
				// allows.
				scrollOffset = max(0, min(scrollOffset, totalBodyLines-bodyAvailHeight))
				needsRedraw = true
				continue

			case 'J': // Jump to message number
				navDir = 1
				e.handleJump(reader, terminal, outputMode, &currentMsgNum, totalMsgCount)
				// Exit scroll loop to load new message
				break scrollLoop

			case 'B', 'F', 'A', '[', ']': // Reply-tree navigation
				if !inThread {
					continue
				}
				target, notice := threadNavTarget(tv, selectedKey, e.Strings())
				if target == 0 {
					e.showReaderNotice(terminal, outputMode, notice, termHeight)
					needsRedraw = true
					continue
				}
				if target > totalMsgCount {
					// A reply posted since the reader started is in the
					// tree but past the bound the reader was given.
					if n, err := e.MessageMgr.GetMessageCountForArea(currentAreaID); err == nil && n > totalMsgCount {
						totalMsgCount = n
					}
				}
				navDir = 1
				currentMsgNum = target
				break scrollLoop

			case 'M': // Mail reply (deferred)
				e.showReaderNotice(terminal, outputMode, e.Strings().MsgMailReplyDeferred, termHeight)
				needsRedraw = true
				continue

			case 'L': // List messages in current area, in the caller's last list mode
				if opts.fromList {
					// The list under this reader shows itself again, with
					// the cursor on this message.
					return currentUser, "LIST", currentMsgNum, nil
				}
				updatedUser, nextAction, listMsg, listErr := runListMsgsWith(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, "", baseFilter,
					listOpts{fromReader: true, startMsg: currentMsgNum})
				if updatedUser != nil {
					currentUser = updatedUser
				}
				if listErr != nil || nextAction == "LOGOFF" {
					return currentUser, "LOGOFF", currentMsgNum, listErr
				}
				matcher = twitMatcher(e, userManager, currentUser)
				// The list picked a message (Enter) or came back (V, Q, Esc)
				// to the one it was opened from; show it here, with the area's
				// count refreshed in case the list showed newer posts.
				if nextAction == "READ" && listMsg > 0 {
					if listMsg > totalMsgCount {
						if n, err := e.MessageMgr.GetMessageCountForArea(currentAreaID); err == nil && n > totalMsgCount {
							totalMsgCount = n
						}
					}
					navDir = 1
					currentMsgNum = listMsg
				}
				break scrollLoop

			case 'D': // Delete message (sysop/co-sysop only)
				if !isSysop {
					continue // Ignore if triggered without access
				}
				confirmed, confErr := promptSingleChar(reader, terminal,
					"\r\n |04Delete this message? [Y/N] |07", outputMode)
				if confErr != nil || confirmed != 'Y' {
					needsRedraw = true
					continue
				}
				if delErr := e.MessageMgr.DeleteMessage(currentAreaID, currentMsg.MsgNum); delErr != nil {
					slog.Error("delete message", "node", nodeNumber, "msg", currentMsg.MsgNum, "area", currentAreaID, "error", delErr)
					terminalio.WriteProcessedBytes(terminal,
						ansi.ReplacePipeCodes([]byte("\r\n|01Error deleting message.|07\r\n")), outputMode)
					uiPause(1 * time.Second)
					needsRedraw = true
					continue
				}
				// Pack the base (physically remove deleted messages and
				// renumber) then rebuild reply threading chains.
				if packErr := e.MessageMgr.PackAndLinkArea(currentAreaID); packErr != nil {
					slog.Error("pack/link area after delete", "node", nodeNumber, "area", currentAreaID, "error", packErr)
				}
				// Refresh total count after pack renumbered messages
				newTotal, _ := e.MessageMgr.GetMessageCountForArea(currentAreaID)
				if newTotal > 0 {
					totalMsgCount = newTotal
				}
				// After pack, the deleted slot is gone. The message that
				// was at currentMsgNum+1 is now at currentMsgNum, so keep
				// the same position. If we deleted the last message, clamp.
				if currentMsgNum > totalMsgCount {
					currentMsgNum = totalMsgCount
				}
				if totalMsgCount <= 0 {
					break readerLoop
				}
				break scrollLoop

			case 'K': // Kludges: control information (sysop/co-sysop only)
				if !isSysop {
					continue
				}
				kludges, kErr := e.MessageMgr.GetMessageKludges(currentAreaID, currentMsg.MsgNum)
				if kErr != nil {
					slog.Error("read message kludges", "node", nodeNumber, "msg", currentMsg.MsgNum, "area", currentAreaID, "error", kErr)
					e.showReaderNotice(terminal, outputMode, "|01Could not read this message's control information.|07", termHeight)
					needsRedraw = true
					continue
				}
				if vErr := showKludgeView(sessionIH, terminal, outputMode, currentMsg.MsgNum, kludges, termWidth, termHeight); vErr != nil {
					if errors.Is(vErr, editor.ErrIdleTimeout) {
						return nil, "LOGOFF", currentMsgNum, editor.ErrIdleTimeout
					}
					if errors.Is(vErr, io.EOF) {
						return nil, "LOGOFF", currentMsgNum, io.EOF
					}
				}
				needsRedraw = true
				continue

			case 'Q': // Quit
				quitNewscan = true
				break readerLoop

			case '?': // Help
				displayReaderHelp(terminal, outputMode, isSysop, twitOn)
				needsRedraw = true
				continue

			default:
				continue
			}
		}
	}

	// Update lastread on exit (advance only, as above)
	if currentMsgNum >= 1 && currentMsgNum <= totalMsgCount {
		if lrErr := e.MessageMgr.AdvanceLastRead(currentAreaID, currentUser.Handle, currentMsgNum); lrErr != nil {
			slog.Error("failed to update last read on exit", "node", nodeNumber, "error", lrErr)
		}
	}

	if quitNewscan {
		return nil, "QUIT_NEWSCAN", currentMsgNum, nil
	}
	return nil, "", currentMsgNum, nil
}

// readerThreadView builds the thread view for the message the reader is on.
// Only the messages of that thread are loaded, each once, and only those
// filter accepts are shown; cur is reused rather than read again.
func readerThreadView(e *MenuExecutor, areaID, msgNum int, cur *message.DisplayMessage, filter msgOwnershipFilter) *threadView {
	nodes, err := e.MessageMgr.ReplyTree(areaID)
	if err != nil {
		slog.Warn("failed to build reply tree", "area", areaID, "error", err)
		nodes = nil
	}
	load := func(n int) *message.DisplayMessage {
		if n == msgNum {
			return cur
		}
		m, err := e.MessageMgr.GetMessage(areaID, n)
		if err != nil || (filter != nil && !filter(m)) {
			return nil
		}
		return m // deleted ones too, flagged, for the view to draw as placeholders
	}
	return newThreadView(nodes, msgNum, load)
}

var quotePrefixRe = regexp.MustCompile(`^[A-Za-z0-9]{1,3}>`)

// MessageHeaderTemplate represents a discovered message header template file.
type MessageHeaderTemplate struct {
	Number      int    // Template number (e.g., 1, 2, 15)
	Filename    string // Full filename (e.g., "MSGHDR.15.ans")
	DisplayName string // Human-readable name (e.g., "Header Style 15")
}

// headerDisplayNames maps template numbers to their actual display names.
var headerDisplayNames = map[int]string{
	1:  "Generic Blue Box",
	2:  "Extremely Simple Message Header",
	3:  "LiQUiD Blue Box Header",
	4:  "Generic TCS Header",
	5:  "Gray/White ViSiON/2 Header",
	6:  "Cool V/2 Quick Header!",
	7:  "ViSiON-X Header",
	8:  "Bouncer Neato Header",
	9:  "Celerity Header",
	10: "PC Express Header",
	11: "Extreme Header",
	12: "LSD Header",
	13: "Renegade Header",
	14: "ViSiON-X Grey/White Header",
}

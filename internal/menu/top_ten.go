package menu

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/util"
)

// Top Ten (#956, ViSiON/2's ST): the board's ten users with the most of
// one thing, posts by default. RUN:TOPTEN takes the list to show as its
// argument. The screen is TOPTEN.TOP, one TOPTEN.MID row per user and
// TOPTEN.BOT, from the theme's templates.

// topTenList is one list RUN:TOPTEN can show.
type topTenList struct {
	// arg is the list's name as RUN:TOPTEN takes it.
	arg string
	// title is the list's heading, from strings.json.
	title func(*config.StringsConfig) string
	// value is what a user is ranked by.
	value func(*user.User) int64
	// bytes says value is a byte count, shown as a size.
	bytes bool
}

// topTenLists are the lists, the default first.
var topTenLists = []topTenList{
	{"posts", func(s *config.StringsConfig) string { return s.TopTenPosts }, func(u *user.User) int64 { return int64(u.MessagesPosted) }, false},
	{"calls", func(s *config.StringsConfig) string { return s.TopTenCalls }, func(u *user.User) int64 { return int64(u.TimesCalled) }, false},
	{"uploads", func(s *config.StringsConfig) string { return s.TopTenUploads }, func(u *user.User) int64 { return int64(u.NumUploads) }, false},
	{"downloads", func(s *config.StringsConfig) string { return s.TopTenDownloads }, func(u *user.User) int64 { return int64(u.NumDownloads) }, false},
	{"uploaded", func(s *config.StringsConfig) string { return s.TopTenUploaded }, func(u *user.User) int64 { return u.UploadedBytes }, true},
	{"downloaded", func(s *config.StringsConfig) string { return s.TopTenDownloaded }, func(u *user.User) int64 { return u.DownloadedBytes }, true},
}

// topTenRows is how many users a list shows.
const topTenRows = 10

// topTenListFor is the list args names, the default for none. ok is false
// for a name that isn't a list; the default is returned with it.
func topTenListFor(args string) (topTenList, bool) {
	name := strings.ToLower(strings.TrimSpace(args))
	if name == "" {
		return topTenLists[0], true
	}
	for _, l := range topTenLists {
		if l.arg == name {
			return l, true
		}
	}
	return topTenLists[0], false
}

// topTenEntry is one row of a list.
type topTenEntry struct {
	rank  int
	user  *user.User
	value int64
}

// topTenRank is the n users of users with the most of l's value: active
// accounts only (not deleted, banned or waiting for validation), and only
// those with some. Equal values go by user number, so the list reads the
// same every time.
func topTenRank(users []*user.User, l topTenList, n int) []topTenEntry {
	var out []topTenEntry
	for _, u := range users {
		if u == nil || u.DeletedUser || !u.Validated || u.AccessLevel <= 0 {
			continue
		}
		if v := l.value(u); v > 0 {
			out = append(out, topTenEntry{user: u, value: v})
		}
	}
	slices.SortFunc(out, func(a, b topTenEntry) int {
		if a.value != b.value {
			if a.value > b.value {
				return -1
			}
			return 1
		}
		return a.user.ID - b.user.ID
	})
	if len(out) > n {
		out = out[:n]
	}
	for i := range out {
		out[i].rank = i + 1
	}
	return out
}

// topTenValue is how a row shows its value: a count, or a size for bytes.
func topTenValue(l topTenList, v int64) string {
	if l.bytes {
		return util.FormatFileSize(v)
	}
	return strconv.FormatInt(v, 10)
}

// defaultTopTenMid is the row when the theme has no TOPTEN.MID.
const defaultTopTenMid = "|08 @RK:2@. |15@UN:25@ |11@VA:-10@|07"

// renderTopTenTokens fills a template's @CODE@ and @CODE:width@ tokens:
// TITLE in any of the three, and RK (rank), UN (handle), GL (group or
// location) and VA (value) in a row, row nil for TOP and BOT. A negative
// width, and RK and VA always, align right.
func renderTopTenTokens(template, title string, l topTenList, row *topTenEntry) string {
	return lastCallerATTokenRegex.ReplaceAllStringFunc(template, func(match string) string {
		parts := lastCallerATTokenRegex.FindStringSubmatch(match)
		code := strings.ToUpper(parts[1])
		var value string
		right, caller := false, false
		switch {
		case code == "TITLE":
			value = title
		case row == nil:
			return match
		case code == "RK":
			value, right = strconv.Itoa(row.rank), true
		case code == "UN":
			value, caller = row.user.Handle, true
		case code == "GL":
			value, caller = row.user.GroupLocation, true
		case code == "VA":
			value, right = topTenValue(l, row.value), true
		default:
			return match
		}
		if parts[2] != "" {
			if width, err := strconv.Atoi(parts[2]); err == nil {
				value = formatLastCallerATWidth(value, width, right)
			}
		}
		if caller {
			// Cut to width first, then escaped (see callerText).
			value = callerText(value)
		}
		return value
	})
}

// topTenTemplate reads the theme's template name, "" when it has none.
func (e *MenuExecutor) topTenTemplate(name string, node int) string {
	path := e.templateFile(name)
	b, err := readTemplateFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to read template", "node", node, "path", path, "error", err)
		}
		return ""
	}
	return string(normalizePipeCodeDelimiters(stripSauceMetadata(b)))
}

// runTopTen shows one Top Ten list, then waits for Enter.
func runTopTen(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	if c.currentUser == nil || c.userManager == nil {
		return nil, "", nil
	}
	l, ok := topTenListFor(args)
	if !ok {
		slog.Warn("TOPTEN: unknown list, showing the default", "node", c.nodeNumber, "list", args, "default", l.arg)
	}
	title := l.title(e.Strings())
	rows := topTenRank(c.userManager.GetAllUsers(), l, topTenRows)

	top := e.topTenTemplate("TOPTEN.TOP", c.nodeNumber)
	mid := e.topTenTemplate("TOPTEN.MID", c.nodeNumber)
	bot := e.topTenTemplate("TOPTEN.BOT", c.nodeNumber)
	if strings.TrimSpace(mid) == "" {
		mid = defaultTopTenMid
	}
	if top == "" {
		top = "|15@TITLE@|07"
	}

	var out bytes.Buffer
	line := func(s string) {
		out.Write(ansi.ReplacePipeCodes([]byte(strings.TrimRight(s, "\r\n"))))
		out.WriteString("\r\n")
	}
	line(renderTopTenTokens(top, title, l, nil))
	for i := range rows {
		line(renderTopTenTokens(mid, title, l, &rows[i]))
	}

	write := func(b []byte) error {
		if c.outputMode == ansi.OutputModeCP437 {
			_, err := c.terminal.Write(b)
			return err
		}
		return terminalio.WriteProcessedBytes(c.terminal, b, c.outputMode)
	}
	if err := terminalio.WriteProcessedBytes(c.terminal, []byte(ansi.ClearScreen()), c.outputMode); err != nil {
		return nil, "", err
	}
	if err := write(out.Bytes()); err != nil {
		return nil, "", err
	}
	if len(rows) == 0 {
		e.writeString(c.terminal, c.outputMode, e.Strings().TopTenNobody)
	}
	if bot != "" {
		if err := write(ansi.ReplacePipeCodes([]byte(renderTopTenTokens(bot, title, l, nil)))); err != nil {
			return nil, "", err
		}
	}

	pausePrompt := e.Strings().PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	if err := e.writeCenteredPausePrompt(c.s, c.terminal, pausePrompt, c.outputMode, c.termWidth, c.termHeight); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", err
	}
	return nil, "", nil
}

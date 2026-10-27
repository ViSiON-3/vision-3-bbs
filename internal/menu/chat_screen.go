package menu

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// A chat screen is a sysop-designed split-screen chat: ANSI art for the
// background and a layout saying where each side types and where the names,
// time, date and elapsed minutes go. A screen called NAME is the pair
// cfg/NAME.json (the layout) and ansi/NAME.ANS (the art) in the menu set,
// overlay first like every other menu file. Vision/2 kept the same two files
// as CHATNFO.n and CHAT.n; ConvertChatNfo reads the old layout format.

// ChatScreenBuiltin names the fixed layout: a divider across the middle with
// the sysop above and the caller below. A sysop picks it from the WFC to
// override a menu set whose default is a designed screen.
const ChatScreenBuiltin = "builtin"

// chatSegment is one typing line: the cells from column From to column To,
// both included, on row Row. Coordinates are 1-based.
type chatSegment struct {
	Row  int `json:"row"`
	From int `json:"from"`
	To   int `json:"to"`
}

func (s chatSegment) width() int { return s.To - s.From + 1 }

// chatField places one status value. Width, when set, pads or cuts the value
// to that many columns so a changing value never leaves part of an old one
// behind or runs into the art.
type chatField struct {
	Row   int `json:"row"`
	Col   int `json:"col"`
	Width int `json:"width,omitempty"`
}

// chatSide is one party's typing area and text colour. Color is a DOS
// attribute as in theme.json; 0 takes the theme's chat colour for that side.
type chatSide struct {
	Color int           `json:"color,omitempty"`
	Lines []chatSegment `json:"lines"`
}

// chatFields are the status values the screen can show. A nil field is not
// drawn; the art can carry the names itself through the @SYSOP@ and @CALLER@
// placeholders instead.
type chatFields struct {
	SysopName  *chatField `json:"sysopName,omitempty"`
	CallerName *chatField `json:"callerName,omitempty"`
	Time       *chatField `json:"time,omitempty"`
	Date       *chatField `json:"date,omitempty"`
	Elapsed    *chatField `json:"elapsed,omitempty"`
}

// chatLayout is a chat screen's layout, as read from cfg/NAME.json.
type chatLayout struct {
	// Title says what the screen looks like, for the sysop picking one; it
	// is not drawn.
	Title string `json:"title,omitempty"`
	// Art is the background's file name in ansi/; empty means NAME.ANS.
	Art string `json:"art,omitempty"`
	// Width and Height are the terminal size the screen is drawn for,
	// 80x25 when left out. A caller with a smaller terminal gets the
	// built-in layout instead.
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
	// StatusColor is the attribute the fields are drawn in; 0 means grey.
	StatusColor int        `json:"statusColor,omitempty"`
	Sysop       chatSide   `json:"sysop"`
	Caller      chatSide   `json:"caller"`
	Fields      chatFields `json:"fields"`

	name string
	art  []byte // the background, CP437 with SAUCE stripped; nil for the built-in layout
}

// validate fills in the size and checks every line and field lies on the
// screen. A layout that fails here would draw over the art or off the
// terminal, so it is refused whole.
func (sc *chatLayout) validate() error {
	if sc.Width <= 0 {
		sc.Width = 80
	}
	if sc.Height <= 0 {
		sc.Height = 25
	}
	for _, side := range []struct {
		name string
		side chatSide
	}{{"sysop", sc.Sysop}, {"caller", sc.Caller}} {
		if len(side.side.Lines) == 0 {
			return fmt.Errorf("%s has no typing lines", side.name)
		}
		for i, seg := range side.side.Lines {
			if seg.Row < 1 || seg.Row > sc.Height || seg.From < 1 || seg.To > sc.Width || seg.From > seg.To {
				return fmt.Errorf("%s line %d (row %d, columns %d-%d) is outside the %dx%d screen",
					side.name, i+1, seg.Row, seg.From, seg.To, sc.Width, sc.Height)
			}
		}
	}
	for _, f := range []struct {
		name  string
		field *chatField
	}{{"sysopName", sc.Fields.SysopName}, {"callerName", sc.Fields.CallerName},
		{"time", sc.Fields.Time}, {"date", sc.Fields.Date}, {"elapsed", sc.Fields.Elapsed}} {
		if f.field == nil {
			continue
		}
		if f.field.Row < 1 || f.field.Row > sc.Height || f.field.Col < 1 || f.field.Col > sc.Width || f.field.Width < 0 {
			return fmt.Errorf("field %s (row %d, column %d) is outside the %dx%d screen",
				f.name, f.field.Row, f.field.Col, sc.Width, sc.Height)
		}
	}
	return nil
}

// fits reports whether a terminal of width x height can show the screen.
func (sc *chatLayout) fits(width, height int) bool {
	return sc.Width <= width && sc.Height <= height
}

// parseChatLayout reads a layout. Unknown keys are an error so a misspelt
// field is reported rather than silently ignored.
func parseChatLayout(data []byte) (*chatLayout, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var sc chatLayout
	if err := dec.Decode(&sc); err != nil {
		return nil, err
	}
	if err := sc.validate(); err != nil {
		return nil, err
	}
	return &sc, nil
}

// loadChatLayout reads the screen called name from the menu set: its layout
// from cfg/NAME.json and its art from ansi/.
func loadChatLayout(m menuset.Set, name string) (*chatLayout, error) {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return nil, fmt.Errorf("bad chat screen name %q", name)
	}
	layoutPath, err := m.ResolveFirst("cfg", name+".json", name+".JSON")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(layoutPath)
	if err != nil {
		return nil, err
	}
	sc, err := parseChatLayout(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", layoutPath, err)
	}
	sc.name = name
	artName := sc.Art
	var artPath string
	if artName != "" {
		artPath, err = m.Resolve("ansi", artName)
	} else {
		artPath, err = m.ResolveFirst("ansi", name+".ANS", name+".ans")
	}
	if err != nil {
		return nil, err
	}
	if sc.art, err = ansi.GetAnsiFileContent(artPath); err != nil {
		return nil, err
	}
	return sc, nil
}

// CheckChatScreen loads the chat screen called name the way chat would and
// returns why it cannot be used, for the doctor.
func CheckChatScreen(m menuset.Set, name string) error {
	_, err := loadChatLayout(m, name)
	return err
}

// ChatScreens lists the chat screens a menu set defines: the stems of the
// .json files in its cfg directory, overlay included, sorted by name.
func ChatScreens(m menuset.Set) []string {
	entries, err := m.ReadDir("cfg")
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("listing chat screens", "error", err)
		}
		return nil
	}
	var names []string
	for _, e := range entries {
		if strings.EqualFold(filepath.Ext(e.Name), ".json") {
			names = append(names, strings.TrimSuffix(e.Name, filepath.Ext(e.Name)))
		}
	}
	sort.Strings(names)
	return names
}

// builtinChatLayout is the fixed layout for a width x height terminal: the
// sysop's lines fill the rows above the middle, the caller's the rows below
// it, and the divider row between them carries the header and clock.
func builtinChatLayout(width, height int) *chatLayout {
	top := (height - 1) / 2
	return &chatLayout{
		Width:  width,
		Height: height,
		Sysop:  chatSide{Lines: fullWidthSegments(1, top, width)},
		Caller: chatSide{Lines: fullWidthSegments(top+2, height, width)},
		name:   ChatScreenBuiltin,
	}
}

// fullWidthSegments is one typing line per row from first to last, each the
// whole width.
func fullWidthSegments(first, last, width int) []chatSegment {
	segs := make([]chatSegment, 0, last-first+1)
	for r := first; r <= last; r++ {
		segs = append(segs, chatSegment{Row: r, From: 1, To: width})
	}
	return segs
}

// dividerRow is the row between the built-in layout's panes.
func (sc *chatLayout) dividerRow() int { return sc.Sysop.Lines[len(sc.Sysop.Lines)-1].Row + 1 }

// chooseChatLayout picks the screen for a chat: want when the sysop asked for
// one, else the menu set's default from theme.json, else the built-in layout.
// A screen that cannot be loaded, or that needs more rows or columns than the
// caller's terminal has, gives way to the built-in layout so chat still
// opens; the reason is logged for the sysop.
func chooseChatLayout(env *chatEnv, want string, width, height int) *chatLayout {
	sc, err := resolveChatLayout(env, want, "", width, height)
	if err != nil {
		slog.Warn("chat screen unavailable, using the built-in layout", "error", err)
		return builtinChatLayout(width, height)
	}
	return sc
}

// resolveChatLayout is chooseChatLayout without the fallback: it returns why
// the screen cannot be used, as a chat already open must keep its screen
// rather than drop to the built-in one. ChatScreenRandom picks among the
// menu set's screens that load and fit, other than avoid when there is a
// choice, so a switch to random changes the screen.
func resolveChatLayout(env *chatEnv, want, avoid string, width, height int) (*chatLayout, error) {
	name := want
	if name == "" {
		name = env.theme().SysopChatScreen
	}
	if name == "" || strings.EqualFold(name, ChatScreenBuiltin) || env.menus == nil {
		if strings.EqualFold(name, ChatScreenRandom) {
			return nil, errors.New("no menu set to pick a random chat screen from")
		}
		return builtinChatLayout(width, height), nil
	}
	m := env.menus()
	if strings.EqualFold(name, ChatScreenRandom) {
		return randomChatLayout(m, avoid, width, height)
	}
	sc, err := loadChatLayout(m, name)
	if err != nil {
		return nil, fmt.Errorf("chat screen %s: %w", name, err)
	}
	if !sc.fits(width, height) {
		return nil, fmt.Errorf("chat screen %s is %dx%d, the caller's terminal %dx%d", name, sc.Width, sc.Height, width, height)
	}
	return sc, nil
}

// ChatScreenRandom picks one of the menu set's chat screens each time, as
// the default or the sysop's choice.
const ChatScreenRandom = "random"

// chatPick chooses one of n; tests replace it.
var chatPick = rand.IntN

func randomChatLayout(m menuset.Set, avoid string, width, height int) (*chatLayout, error) {
	var fit []*chatLayout
	for _, name := range ChatScreens(m) {
		sc, err := loadChatLayout(m, name)
		if err != nil {
			slog.Warn("chat screen unavailable for random choice", "screen", name, "error", err)
			continue
		}
		if sc.fits(width, height) {
			fit = append(fit, sc)
		}
	}
	if len(fit) > 1 {
		others := fit[:0:0]
		for _, sc := range fit {
			if !strings.EqualFold(sc.name, avoid) {
				others = append(others, sc)
			}
		}
		if len(others) > 0 {
			fit = others
		}
	}
	if len(fit) == 0 {
		return nil, fmt.Errorf("no chat screen fits the caller's %dx%d terminal", width, height)
	}
	return fit[chatPick(len(fit))], nil
}

// ConvertChatNfo reads a Vision/2 CHATNFO.n layout and returns the equivalent
// chat screen layout as JSON for cfg/NAME.json. The old format is one setting
// per line: TIME=x,y, DATE=x,y, ELAPSED=x,y, SYSOPNAME=x,y and USERNAME=x,y
// place the fields; STATCOLOR=, SYSOPCOLOR= and USERCOLOR= set DOS attributes;
// the lines after SYSOPLINES or USERLINES each give a typing line as
// "n x1,y1 x2,y2" until the other heading or the end. USER_CLEAR_LINE and
// SYSOP_CLEAR_LINE chose how a line was blanked and have no counterpart. The
// art file CHAT.n is used as it is, except that the |U and |S name codes
// become @CALLER@ and @SYSOP@; see ConvertChatArt.
func ConvertChatNfo(r io.Reader) ([]byte, error) {
	sc := chatLayout{Width: 80, Height: 25}
	sysop := map[int]chatSegment{}
	caller := map[int]chatSegment{}
	var section map[int]chatSegment
	xy := func(v string) (*chatField, error) {
		x, y, err := parseXY(v)
		if err != nil {
			return nil, err
		}
		if x == 0 || y == 0 {
			return nil, nil // Vision/2 skipped a field at 0,0
		}
		return &chatField{Row: y, Col: x}, nil
	}
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.ToUpper(strings.TrimSpace(scanner.Text()))
		if line == "" {
			continue
		}
		var err error
		key, value, hasValue := strings.Cut(line, "=")
		switch {
		case line == "SYSOPLINES":
			section = sysop
			continue
		case line == "USERLINES":
			section = caller
			continue
		case line == "USER_CLEAR_LINE" || line == "SYSOP_CLEAR_LINE":
			continue
		case hasValue && key == "TIME":
			sc.Fields.Time, err = xy(value)
		case hasValue && key == "DATE":
			sc.Fields.Date, err = xy(value)
		case hasValue && key == "ELAPSED":
			sc.Fields.Elapsed, err = xy(value)
		case hasValue && key == "SYSOPNAME":
			sc.Fields.SysopName, err = xy(value)
		case hasValue && key == "USERNAME":
			sc.Fields.CallerName, err = xy(value)
		case hasValue && key == "STATCOLOR":
			sc.StatusColor, err = parseColor(value)
		case hasValue && key == "SYSOPCOLOR":
			sc.Sysop.Color, err = parseColor(value)
		case hasValue && key == "USERCOLOR":
			sc.Caller.Color, err = parseColor(value)
		case section != nil:
			var n int
			var seg chatSegment
			n, seg, err = parseChatNfoLine(line)
			if err == nil {
				section[n] = seg
			}
		default:
			err = fmt.Errorf("unrecognised setting %q", line)
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sc.Sysop.Lines = orderedSegments(sysop)
	sc.Caller.Lines = orderedSegments(caller)
	if err := sc.validate(); err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(&sc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// ConvertChatArt turns a Vision/2 CHAT.n background into a chat screen's art:
// the |U and |S codes it used for the caller's and sysop's names become the
// @CALLER@ and @SYSOP@ placeholders. Everything else is kept byte for byte.
func ConvertChatArt(data []byte) []byte {
	data = bytes.ReplaceAll(data, []byte("|U"), []byte("@CALLER@"))
	data = bytes.ReplaceAll(data, []byte("|u"), []byte("@CALLER@"))
	data = bytes.ReplaceAll(data, []byte("|S"), []byte("@SYSOP@"))
	return bytes.ReplaceAll(data, []byte("|s"), []byte("@SYSOP@"))
}

// parseXY reads "x,y" as Vision/2 did: up to two digits each, the pair
// separated by a comma or space.
func parseXY(v string) (x, y int, err error) {
	fields := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("want x,y, got %q", v)
	}
	if x, err = strconv.Atoi(fields[0]); err != nil {
		return 0, 0, fmt.Errorf("bad x in %q", v)
	}
	if y, err = strconv.Atoi(fields[1]); err != nil {
		return 0, 0, fmt.Errorf("bad y in %q", v)
	}
	return x, y, nil
}

func parseColor(v string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 || n > 255 {
		return 0, fmt.Errorf("bad colour %q", v)
	}
	return n, nil
}

// parseChatNfoLine reads a typing line "n x1,y1 x2,y2". Vision/2 took any
// run of digits as a number whatever separated them, and the end row when
// missing was the start row.
func parseChatNfoLine(line string) (int, chatSegment, error) {
	nums := strings.FieldsFunc(line, func(r rune) bool { return r < '0' || r > '9' })
	if len(nums) < 4 || len(nums) > 5 {
		return 0, chatSegment{}, fmt.Errorf("want \"n x1,y1 x2,y2\", got %q", line)
	}
	vals := make([]int, len(nums))
	for i, s := range nums {
		vals[i], _ = strconv.Atoi(s) // digits only, so it cannot fail
	}
	n, x1, y1, x2 := vals[0], vals[1], vals[2], vals[3]
	y2 := y1
	if len(vals) == 5 {
		y2 = vals[4]
	}
	if n < 1 || n > 30 {
		return 0, chatSegment{}, fmt.Errorf("line number %d is not 1-30", n)
	}
	if y2 != y1 {
		return 0, chatSegment{}, fmt.Errorf("line %d runs from row %d to row %d; a typing line is one row", n, y1, y2)
	}
	return n, chatSegment{Row: y1, From: x1, To: x2}, nil
}

// orderedSegments returns the lines by their Vision/2 line number, dropping
// unset ones the way Vision/2 counted only lines with a start column.
func orderedSegments(m map[int]chatSegment) []chatSegment {
	keys := make([]int, 0, len(m))
	for k, seg := range m {
		if seg.From > 0 {
			keys = append(keys, k)
		}
	}
	sort.Ints(keys)
	segs := make([]chatSegment, 0, len(keys))
	for _, k := range keys {
		segs = append(segs, m[k])
	}
	return segs
}

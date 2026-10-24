package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// --- post command ---
//
// `helper post` writes a message into a message area from the command line,
// for scripted announcements, network ads and bulletins. It goes through
// MessageManager exactly as the BBS editor does, so echomail gets its MSGID,
// tearline and origin and leaves on the next `v3mail ftn-scan`, and a post to
// a netmail area with --to "Name@zone:net/node" is a netmail send that the
// tosser routes like any other.

// Body formats. ans is stored byte for byte; asc and txt are plain text whose
// line endings are normalised and which may be re-wrapped with --wrap.
const (
	postFormatANS = "ans"
	postFormatASC = "asc"
	postFormatTXT = "txt"
)

// FTS-0001 field limits for the packed message header. Longer values are cut
// by the packer, so they are refused up front for networked areas.
const (
	ftnMaxNameLen    = 35
	ftnMaxSubjectLen = 71
)

// chrsCP437 marks a networked post's body as CP437, so systems that
// translate character sets leave ANSI art and line drawing alone.
const chrsCP437 = "CHRS: CP437 2"

func cmdPost(args []string) {
	// Opening the message areas logs at INFO; a command-line tool only needs
	// to hear about problems.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	os.Exit(runPost(args, os.Stdin))
}

func runPost(args []string, stdin io.Reader) int {
	fs := flag.NewFlagSet("post", flag.ContinueOnError)
	areaRef := fs.String("area", "", "Message area tag or ID (required)")
	subject := fs.String("subj", "", "Subject (default: the SAUCE title of an .ANS file)")
	file := fs.String("file", "", "Text file to post as the body, or - for stdin (required)")
	to := fs.String("to", message.MsgToUserAll, `Recipient; in a netmail area "Name@zone:net/node"`)
	from := fs.String("from", "", "Sender (default: sysOpName from config.json)")
	private := fs.Bool("private", false, "Mark the message private")
	replyTo := fs.Int("reply-to", 0, "Number of the message in the same area this replies to")
	format := fs.String("format", "", "Body format: ans, asc or txt (default: from the file extension; txt for stdin)")
	charset := fs.String("charset", "auto", "Plain-text input charset: auto, cp437 or utf8 (ans is always stored raw)")
	wrap := fs.Int("wrap", 0, "Re-wrap plain text to this many columns (asc/txt only; 0 = keep lines as they are)")
	dryRun := fs.Bool("dry-run", false, "Check and describe the post without writing it")
	configDir := fs.String("config", "configs", "Config directory")
	dataDir := fs.String("data", "data", "Data directory")
	fs.Usage = func() {
		w := os.Stderr
		_, _ = fmt.Fprintf(w, "Usage: helper post --area TAG --subj SUBJECT --file FILE [options]\n\n")
		_, _ = fmt.Fprintf(w, "Post a text file (.ANS, .ASC or .TXT) as a message in a message area.\n")
		_, _ = fmt.Fprintf(w, "Echomail is exported on the next 'v3mail ftn-scan'. In a netmail area,\n")
		_, _ = fmt.Fprintf(w, "--to \"Name@zone:net/node\" sends netmail.\n\n")
		_, _ = fmt.Fprintf(w, ".ANS files are posted byte for byte (never re-wrapped) with SAUCE removed.\n")
		_, _ = fmt.Fprintf(w, "Plain text has CRLF/LF line endings normalised; UTF-8 is converted to CP437.\n\n")
		_, _ = fmt.Fprintf(w, "Options:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(w, "\nExamples:\n")
		_, _ = fmt.Fprintf(w, "  helper post --area FD_OTHERNETS --subj \"[ANSI] Our Network\" --file ad.ans\n")
		_, _ = fmt.Fprintf(w, "  helper post --area FIDO_NETMAIL --to \"Sean Dennis@1:18/200\" --subj Hello --file note.txt\n")
		_, _ = fmt.Fprintf(w, "  fortune | helper post --area GENERAL --subj \"Today's fortune\" --file -\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		return postUsageError(fs, "unexpected argument %q (one --file per post)", fs.Arg(0))
	}
	if *areaRef == "" {
		return postUsageError(fs, "--area is required")
	}
	if *file == "" {
		return postUsageError(fs, "--file is required")
	}

	fmtName, err := postFormatFor(*file, *format)
	if err != nil {
		return postUsageError(fs, "%v", err)
	}

	var raw []byte
	if *file == "-" {
		raw, err = io.ReadAll(stdin)
	} else {
		raw, err = os.ReadFile(*file)
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", *file, err)
		return 1
	}

	body, err := preparePostBody(raw, postBodyOptions{format: fmtName, charset: *charset, wrap: *wrap})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %s: %v\n", *file, err)
		return 1
	}
	if *subject == "" {
		*subject = body.sauceTitle
	}
	if strings.TrimSpace(*subject) == "" {
		return postUsageError(fs, "--subj is required (the file has no SAUCE title)")
	}

	serverCfg, cfgErr := config.LoadServerConfig(*configDir)
	if *from == "" {
		if cfgErr != nil || serverCfg.SysOpName == "" {
			_, _ = fmt.Fprintf(os.Stderr, "Error: --from is required (no sysOpName in %s)\n", filepath.Join(*configDir, "config.json"))
			return 1
		}
		*from = serverCfg.SysOpName
	}

	mm, err := newHelperMessageManager(*configDir, *dataDir, serverCfg.BoardName)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	defer func() { _ = mm.Close() }()

	area, err := resolvePostArea(mm, *areaRef)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	msgType := jam.DetermineMessageType(area.AreaType, area.EchoTag)
	networked := !msgType.IsLocal()
	isV3Net := strings.EqualFold(area.AreaType, "v3net") || mm.IsV3NetArea(area.ID)
	toName, destAddr := message.SplitNetmailTo(*to)
	switch {
	case msgType.IsNetmail() && destAddr == "":
		_, _ = fmt.Fprintf(os.Stderr, "Error: area %s is netmail; give --to as \"Name@zone:net/node\"\n", area.Tag)
		return 1
	case !msgType.IsNetmail() && destAddr != "":
		_, _ = fmt.Fprintf(os.Stderr, "Error: --to %q has an FTN address, but %s is not a netmail area\n", *to, area.Tag)
		return 1
	}
	if err := checkPostHeader(*from, toName, *subject, networked); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// PRIVMAIL holds only private mail, as the BBS composer treats it.
	isPrivate := *private || strings.EqualFold(area.Tag, "PRIVMAIL")
	if isPrivate {
		if msgType.IsEchomail() || isV3Net {
			// Echomail goes to every link without the private flag, so the
			// post would be public; V3Net never sends private posts, so it
			// would sit unsent in a shared area.
			_, _ = fmt.Fprintf(os.Stderr, "Error: %s is a network area; private messages go in a local or netmail area\n", area.Tag)
			return 1
		}
		if !msgType.IsNetmail() {
			// A private local message is shown only to the handle in its To
			// field, so it has to name an existing account exactly.
			handle, err := resolvePrivateRecipient(filepath.Join(*dataDir, "users"), toName)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return 1
			}
			*to = handle
		}
	}

	opts := message.PostOptions{Private: isPrivate, ReplyToNum: *replyTo}
	if networked && !area.IsQWKNet() {
		opts.Kludges = []string{chrsCP437}
	}
	if *replyTo > 0 {
		parent, err := mm.GetMessage(area.ID, *replyTo)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: --reply-to %d: %v\n", *replyTo, err)
			return 1
		}
		opts.ReplyToMsgID = parent.MsgID
	} else if *replyTo < 0 {
		return postUsageError(fs, "--reply-to must be a message number")
	}

	for _, w := range body.warnings {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
	}
	if destAddr != "" {
		describeNetmailDest(*dataDir, area.Network, destAddr)
	}

	if *dryRun {
		fmt.Printf("Dry run: would post to %s (%s)\n", area.Tag, area.AreaType)
		printPostSummary(*from, *to, *subject, fmtName, body)
		return 0
	}

	msgNum, err := mm.Post(area.ID, *from, *to, *subject, body.text, opts)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error posting to %s: %v\n", area.Tag, err)
		return 1
	}
	fmt.Printf("Posted message #%d to %s (%s)\n", msgNum, area.Tag, area.AreaType)
	printPostSummary(*from, *to, *subject, fmtName, body)
	switch {
	case isV3Net:
		// Written unprocessed; the running BBS's V3Net outbound sweep sends it.
		fmt.Println("  The running BBS tries to send it to the V3Net hub every minute until the hub accepts it.")
	case networked && !area.IsQWKNet():
		fmt.Println("  It is exported on the next 'v3mail ftn-scan'.")
	}
	return 0
}

func postUsageError(fs *flag.FlagSet, format string, a ...any) int {
	_, _ = fmt.Fprintf(os.Stderr, "Error: "+format+"\n\n", a...)
	fs.Usage()
	return 2
}

func printPostSummary(from, to, subject, format string, body postBody) {
	fmt.Printf("  From:    %s\n", from)
	fmt.Printf("  To:      %s\n", to)
	fmt.Printf("  Subject: %s\n", subject)
	fmt.Printf("  Body:    %s, %d lines, %d bytes\n", strings.ToUpper(format), body.lines, len(body.text))
}

// newHelperMessageManager opens the message areas with the origin line text
// of each FTN network, as v3mail does, so echomail written here carries the
// same origin as a post made from the BBS. Its IsV3NetArea hook reports the
// areas v3net.json binds, so posts there are written for the BBS's V3Net
// outbound sweep, as the BBS's own hook does.
func newHelperMessageManager(configDir, dataDir, boardName string) (*message.MessageManager, error) {
	origins := make(map[string]string)
	if ftnCfg, err := loadFTNConfig(filepath.Join(configDir, "ftn.json")); err == nil {
		for name, net := range ftnCfg.Networks {
			origins[name] = net.Origin
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("loading ftn.json: %w", err)
	}
	if boardName == "" {
		boardName = "Vision3 BBS"
	}
	mm, err := message.NewMessageManager(dataDir, configDir, boardName, origins)
	if err != nil {
		return nil, fmt.Errorf("opening message areas: %w", err)
	}
	// Areas bound to V3Net by tag in v3net.json, whatever their type and
	// whether V3Net is enabled, as the BBS sees them: a post there must be
	// left for its outbound sweep.
	v3netCfg, err := config.LoadV3NetConfig(configDir)
	if err != nil {
		_ = mm.Close()
		return nil, fmt.Errorf("loading v3net.json: %w", err)
	}
	mm.IsV3NetArea = message.V3NetBoundAreas(mm, v3netCfg.Leaves)
	return mm, nil
}

// resolvePostArea finds an area by tag (case-insensitive) or numeric ID.
func resolvePostArea(mm *message.MessageManager, ref string) (*message.MessageArea, error) {
	if id, err := strconv.Atoi(ref); err == nil {
		if area, ok := mm.GetAreaByID(id); ok {
			return area, nil
		}
		return nil, fmt.Errorf("no message area with ID %d", id)
	}
	for _, area := range mm.ListAreas() {
		if strings.EqualFold(area.Tag, ref) {
			return area, nil
		}
	}
	return nil, fmt.Errorf("no message area with tag %q (see 'helper areas list')", ref)
}

// resolvePrivateRecipient returns the account handle a private local message
// to name must carry: the handle of the user with that handle
// (case-insensitive). It fails when there is no such account or it has been
// deleted, since the message would then be readable by no one.
func resolvePrivateRecipient(usersDir, name string) (string, error) {
	um, err := user.NewUserManager(usersDir)
	if err != nil {
		return "", fmt.Errorf("loading users from %s: %w", usersDir, err)
	}
	u, ok := um.GetUser(strings.TrimSpace(name))
	if !ok || u.DeletedUser {
		return "", fmt.Errorf("private mail needs a user handle in --to; no user %q", name)
	}
	return u.Handle, nil
}

// checkPostHeader rejects header fields the message base or a packed FTN
// message cannot carry: empty names, control characters, and, for networked
// areas, characters CP437 has no byte for and values longer than the
// FTS-0001 header fields. The exporter writes networked headers as CP437
// (ftn.EncodeFTNString), one byte per character, so the limits count
// characters, and a character CP437 lacks would leave as '?'.
func checkPostHeader(from, to, subject string, networked bool) error {
	for _, f := range []struct{ name, val string }{{"--from", from}, {"--to", to}, {"--subj", subject}} {
		if strings.TrimSpace(f.val) == "" {
			return fmt.Errorf("%s is empty", f.name)
		}
		if strings.ContainsFunc(f.val, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return fmt.Errorf("%s contains control characters", f.name)
		}
	}
	if !networked {
		return nil
	}
	for _, f := range []struct{ name, val string }{{"--from", from}, {"--to", to}, {"--subj", subject}} {
		if i := strings.IndexFunc(f.val, func(r rune) bool { return !ftn.CP437Rune(r) }); i >= 0 {
			r, _ := utf8.DecodeRuneInString(f.val[i:])
			return fmt.Errorf("%s: %q (U+%04X) has no CP437 equivalent, so FTN headers cannot carry it", f.name, r, r)
		}
	}
	if n := utf8.RuneCountInString(from); n > ftnMaxNameLen {
		return fmt.Errorf("--from is %d characters; FTN allows %d", n, ftnMaxNameLen)
	}
	if n := utf8.RuneCountInString(to); n > ftnMaxNameLen {
		return fmt.Errorf("--to is %d characters; FTN allows %d", n, ftnMaxNameLen)
	}
	if n := utf8.RuneCountInString(subject); n > ftnMaxSubjectLen {
		return fmt.Errorf("--subj is %d characters; FTN allows %d", n, ftnMaxSubjectLen)
	}
	return nil
}

// describeNetmailDest prints the nodelist entry for a netmail destination so
// the sysop can see whose system the message is going to, or warns when the
// address is not listed.
func describeNetmailDest(dataDir, network, dest string) {
	addr, err := ftn.ParseAddress(dest)
	if err != nil {
		return
	}
	index := ftn.NewNodelistIndex(ftn.NodelistDir(dataDir))
	if index.List(network) == nil {
		_, _ = fmt.Fprintf(os.Stderr, "Note: no compiled nodelist for %s; %s was not checked\n", network, dest)
		return
	}
	node, ok := index.Lookup(network, addr)
	if !ok {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: %s is not in the %s nodelist\n", dest, network)
		return
	}
	fmt.Printf("Netmail to %s: %s", dest, node.Name)
	if node.Sysop != "" {
		fmt.Printf(", sysop %s", node.Sysop)
	}
	if node.Location != "" {
		fmt.Printf(" (%s)", node.Location)
	}
	fmt.Println()
}

// postFormatFor picks the body format: the --format value when given,
// otherwise the file's extension, with anything that is not .ANS or .ASC
// (and stdin) treated as plain text.
func postFormatFor(file, override string) (string, error) {
	if override != "" {
		switch f := strings.ToLower(override); f {
		case postFormatANS, postFormatASC, postFormatTXT:
			return f, nil
		default:
			return "", fmt.Errorf("unknown --format %q (want ans, asc or txt)", override)
		}
	}
	switch strings.ToLower(filepath.Ext(file)) {
	case ".ans":
		return postFormatANS, nil
	case ".asc":
		return postFormatASC, nil
	default:
		return postFormatTXT, nil
	}
}

// postBodyOptions controls preparePostBody.
type postBodyOptions struct {
	format  string // postFormatANS, postFormatASC or postFormatTXT
	charset string // "auto", "cp437" or "utf8"; ignored for ans
	wrap    int    // re-wrap width for plain text; 0 keeps lines as they are
}

// postBody is a file made ready to post.
type postBody struct {
	text       string   // LF-separated body; the message base stores LF as CR
	lines      int      // number of lines in text
	sauceTitle string   // title from the file's SAUCE record, "" if none
	warnings   []string // content the sysop should look at; nothing was changed for them
}

// preparePostBody turns a file's bytes into a message body. SAUCE and the
// DOS EOF marker are removed and CRLF, LF and bare CR all become one line
// break. An ans body is otherwise left byte for byte — escape sequences,
// CP437 and long lines included — and gets a trailing attribute reset so the
// tearline and origin appended after it are not drawn in the art's last
// colour. Plain text is converted from UTF-8 to CP437 when charset says so
// (or, for auto, when it is valid UTF-8 with non-ASCII characters) and is
// re-wrapped when wrap > 0. Errors are returned for content that cannot be
// posted: NUL bytes, UTF-8 with no CP437 equivalent, --wrap on ans, and an
// empty body.
func preparePostBody(raw []byte, opts postBodyOptions) (postBody, error) {
	var out postBody
	if opts.format == postFormatANS && opts.wrap > 0 {
		return out, errors.New("--wrap would break ANSI art; it only applies to asc and txt")
	}
	if opts.wrap < 0 {
		return out, errors.New("--wrap must be 0 or more")
	}

	out.sauceTitle = sauceTitle(raw)
	data := ansi.StripSAUCE(raw)
	if i := bytes.IndexByte(data, 0x1A); i >= 0 {
		data = data[:i] // DOS EOF: nothing after it was meant to be shown
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return out, errors.New("contains NUL bytes; it is not a text file")
	}

	if opts.format != postFormatANS {
		var err error
		data, err = toCP437(data, opts.charset)
		if err != nil {
			return out, err
		}
	}

	text := string(data)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return out, errors.New("is empty")
	}

	lines := strings.Split(text, "\n")
	if opts.wrap > 0 {
		lines = wrapPlainLines(lines, opts.wrap)
	}
	out.warnings = postBodyWarnings(lines)

	text = strings.Join(lines, "\n")
	if opts.format == postFormatANS && strings.IndexByte(text, 0x1B) >= 0 {
		text += "\x1b[0m"
	}
	out.text = text
	out.lines = len(lines)
	return out, nil
}

// sauceTitle returns the trimmed Title field of a file's SAUCE record, or ""
// when the file has none.
func sauceTitle(data []byte) string {
	const sauceSize = 128
	if len(data) < sauceSize {
		return ""
	}
	rec := data[len(data)-sauceSize:]
	if !bytes.HasPrefix(rec, []byte("SAUCE")) {
		return ""
	}
	// SAUCE: ID(5) Version(2) Title(35), space or NUL padded, CP437.
	title := strings.TrimRight(string(rec[7:42]), " \x00")
	var b strings.Builder
	for i := 0; i < len(title); i++ {
		c := title[i]
		switch {
		case c < 0x20:
			continue
		case c < 0x80:
			b.WriteByte(c)
		default:
			b.WriteRune(ansi.Cp437ToUnicode[c])
		}
	}
	return strings.TrimSpace(b.String())
}

// toCP437 returns plain text as CP437 bytes. charset "cp437" keeps the bytes
// as they are; "utf8" converts and fails on characters CP437 lacks; "auto"
// converts only text that is valid UTF-8 and has non-ASCII characters, since
// anything else cannot be UTF-8.
func toCP437(data []byte, charset string) ([]byte, error) {
	switch strings.ToLower(charset) {
	case "cp437":
		return data, nil
	case "auto", "":
		if !utf8.Valid(data) || isASCII(data) {
			return data, nil
		}
	case "utf8", "utf-8":
		if !utf8.Valid(data) {
			return nil, errors.New("is not valid UTF-8 (use --charset cp437)")
		}
	default:
		return nil, fmt.Errorf("unknown --charset %q (want auto, cp437 or utf8)", charset)
	}

	out := make([]byte, 0, len(data))
	line := 1
	for _, r := range string(data) {
		switch {
		case r == '\n':
			line++
			out = append(out, '\n')
		case r < 0x80:
			out = append(out, byte(r))
		default:
			b, ok := ansi.UnicodeToCP437[r]
			if !ok {
				return nil, fmt.Errorf("line %d: %q (U+%04X) has no CP437 equivalent", line, r, r)
			}
			out = append(out, b)
		}
	}
	return out, nil
}

func isASCII(data []byte) bool {
	for _, b := range data {
		if b >= 0x80 {
			return false
		}
	}
	return true
}

// wrapPlainLines word-wraps each line to width columns. Lines that already
// fit are kept, blank lines stay as paragraph breaks, and a word longer than
// width is split.
func wrapPlainLines(lines []string, width int) []string {
	var out []string
	for _, line := range lines {
		if len(line) <= width {
			out = append(out, line)
			continue
		}
		var cur string
		// Split on ASCII blanks only: the line is CP437, and byte pairs
		// such as C2 A0 (┬á) would otherwise be read as a UTF-8 space.
		for _, word := range strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' }) {
			for len(word) > width {
				if cur != "" {
					out = append(out, cur)
					cur = ""
				}
				out = append(out, word[:width])
				word = word[width:]
			}
			switch {
			case cur == "":
				cur = word
			case len(cur)+1+len(word) <= width:
				cur += " " + word
			default:
				out = append(out, cur)
				cur = word
			}
		}
		if cur != "" {
			out = append(out, cur)
		}
	}
	return out
}

// postBodyWarnings flags body lines that FTN software reads as message
// structure, and screen-control sequences that reach past the message.
func postBodyWarnings(lines []string) []string {
	var warns []string
	for i, line := range lines {
		plain := strings.TrimLeft(stripEscapes(line), " ")
		n := i + 1
		switch {
		case plain == "---" || strings.HasPrefix(plain, "--- "):
			warns = append(warns, fmt.Sprintf("line %d looks like a tearline; readers and tossers may treat the rest of the message as its trailer", n))
		case strings.HasPrefix(plain, "* Origin:"):
			warns = append(warns, fmt.Sprintf("line %d looks like an origin line; tossers may take the origin address from it", n))
		case strings.HasPrefix(plain, "SEEN-BY:"):
			warns = append(warns, fmt.Sprintf("line %d starts with SEEN-BY:; tossers strip such lines", n))
		}
	}
	all := strings.Join(lines, "\n")
	if strings.Contains(all, "\x1b[2J") {
		warns = append(warns, "the body clears the screen (ESC[2J); some readers lose the message header")
	}
	if strings.Contains(all, "\x1b[M") || strings.Contains(all, "\x1b[N") {
		warns = append(warns, "the body looks like it contains ANSI music (ESC[M / ESC[N)")
	}
	return warns
}

// stripEscapes removes ANSI CSI sequences, leaving the visible text.
func stripEscapes(s string) string {
	if strings.IndexByte(s, 0x1B) < 0 {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1B && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7E) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

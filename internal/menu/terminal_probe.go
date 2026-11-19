package menu

import (
	"bytes"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// replyWindow is how long after the probe the session's input waits longer
// on an ESC for a reply that may follow (see editor.ReplyTrap). It ends
// early when the primary reply arrives. Replies are taken out of the input
// for the whole session either way. A sixel or audio answer that comes
// after the probe's wait but within replyWindow still turns that
// capability on.
const replyWindow = 30 * time.Second

// probeReport reports whether a cursor-report-shaped sequence can be the
// answer to EncodingProbe. One from row 1 cannot (the probe draws on
// EncodingProbeRow) and is a key, such as Shift-F3 (CSI 1 ; 2 R). Anything
// else is taken, so a malformed report never reaches a prompt.
func probeReport(report []byte) bool {
	row, _, ok := ansi.ParseCPR(report)
	return !ok || row != 1
}

// sessionReplyTraps holds, per session, the trap that takes device
// attributes replies out of the input. getSessionIH gives it to each input
// handler it creates.
var sessionReplyTraps sync.Map

// sessionSixelCaps holds, per session, whether the terminal said at connect,
// or within replyWindow after, that it draws sixel graphics. A session with no entry was not probed.
var sessionSixelCaps sync.Map

// sessionAudioCaps holds, per session, whether the terminal said at connect,
// or within replyWindow after, that it plays the board's audio format. A session with no entry was not
// probed.
var sessionAudioCaps sync.Map

// storeSessionAudio records the probe's audio answer for s.
func storeSessionAudio(s ssh.Session, audio bool) {
	sessionAudioCaps.Store(unwrapSession(s), audio)
}

// sessionAudio reports whether s's terminal said at connect that libsndfile
// is present and that it plays WAV with 8-bit unsigned PCM. No answer means
// it does not.
func sessionAudio(s ssh.Session) bool {
	if s == nil {
		return false
	}
	v, ok := sessionAudioCaps.Load(unwrapSession(s))
	return ok && v.(bool)
}

// storeSessionSixel records the probe's answer for s.
func storeSessionSixel(s ssh.Session, sixel bool) {
	sessionSixelCaps.Store(unwrapSession(s), sixel)
}

// sessionSixel reports whether to send s sixel graphics: u's setting when it
// forces them on or off, otherwise what the terminal answered at connect.
// u is nil before login.
func sessionSixel(s ssh.Session, u *user.User) bool {
	if u != nil {
		switch u.PreferredSixel {
		case user.SixelOn:
			return true
		case user.SixelOff:
			return false
		}
	}
	if s == nil {
		return false
	}
	v, ok := sessionSixelCaps.Load(unwrapSession(s))
	return ok && v.(bool)
}

// replyTrapFor returns s's reply trap, or nil before the probe.
func replyTrapFor(s ssh.Session) *editor.ReplyTrap {
	if v, ok := sessionReplyTraps.Load(s); ok {
		return v.(*editor.ReplyTrap)
	}
	return nil
}

// daReplies collects the terminal's replies for one probe: the cursor
// position report for the encoding, device attributes and the audio
// queries.
type daReplies struct {
	log       *slog.Logger
	session   ssh.Session // where decide and late answers store sixel and audio
	lateUntil time.Time   // end of replyWindow; later replies change nothing
	encoding  bool        // the encoding probe was sent
	trap      *editor.ReplyTrap

	mu         sync.Mutex
	cpr        []byte
	ext        []byte
	primary    []byte
	libsndfile []byte // reply to ansi.AudioLibsndfileQuery
	wavU8      []byte // reply to ansi.AudioWAVU8Query
	decided    bool
	answered   chan struct{} // closed when the primary reply arrives
}

// add takes a reply from the input goroutine. Once the probe has decided,
// a sixel or audio answer that comes before lateUntil turns that capability
// on (see applyLate); a late cursor report leaves the encoding as decided,
// because the first screen is drawn in it as soon as the probe returns. A
// reply after lateUntil, or one that is not an answer to the probe's
// queries, is logged and changes nothing.
func (d *daReplies) add(reply []byte, complete bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !complete {
		d.log.Info("incomplete terminal reply dropped", "reply", escapeReply(reply))
		return
	}
	if time.Now().After(d.lateUntil) {
		d.log.Info("terminal reply after the probe ended, ignored", "reply", escapeReply(reply))
		return
	}
	if d.decided && reply[len(reply)-1] == 'R' {
		d.log.Info("late cursor report, encoding left as decided", "reply", escapeReply(reply))
		return
	}
	if d.take(reply) {
		if d.decided {
			d.applyLate(reply)
		}
		return
	}
	d.log.Info("unexpected terminal reply, ignored", "reply", escapeReply(reply))
}

// take records reply as the answer it is, and reports whether it was one
// still missing.
func (d *daReplies) take(reply []byte) bool {
	// Every query was sent before the primary one; after the primary reply
	// the answer is settled.
	switch {
	case d.primary != nil:
	case reply[len(reply)-1] == 'R':
		if d.encoding && d.cpr == nil {
			d.cpr = reply
			return true
		}
	case ansi.IsAudioLibsndfileReply(reply):
		if d.libsndfile == nil {
			d.libsndfile = reply
			return true
		}
	case ansi.IsAudioWAVU8Reply(reply):
		if d.wavU8 == nil {
			d.wavU8 = reply
			return true
		}
	case reply[len(reply)-1] != 'c':
	case reply[2] == '<':
		if d.ext == nil {
			d.ext = reply
			return true
		}
	case reply[2] == '?' || reply[2] == '=':
		if d.primary == nil {
			d.primary = reply
			close(d.answered)
			if d.decided {
				d.trap.StopExpecting()
			}
			// A cursor report should have come before this; one that comes
			// later is still taken out of the input for the rest of
			// replyWindow, and does not change the encoding.
			return true
		}
	}
	return false
}

// decide fixes the answers from the replies received so far: the encoding
// (OutputModeAuto when the encoding probe was not sent), sixel and audio,
// and stores sixel and audio on the session. Storing them under d.mu keeps
// a late answer from being overwritten by this one.
func (d *daReplies) decide() ansi.OutputMode {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.decided = true
	mode := ansi.OutputModeAuto
	if d.encoding {
		mode = encodingFromReport(d.cpr)
	}
	sixel := sixelFromReplies(d.ext, d.primary)
	audio := audioFromReplies(d.libsndfile, d.wavU8)
	d.log.Info("terminal probe", "cursor", escapeReply(d.cpr), "encoding", probeModeName(mode),
		"extended", escapeReply(d.ext), "primary", escapeReply(d.primary), "sixel", sixel,
		"libsndfile", escapeReply(d.libsndfile), "wav_u8", escapeReply(d.wavU8), "audio", audio)
	storeSessionSixel(d.session, sixel)
	storeSessionAudio(d.session, audio)
	return mode
}

// applyLate turns sixel or audio on for the session when the replies, with
// the late reply just taken, now say the terminal has it. It is called with
// d.mu held, after decide. It only changes an answer decide stored, from
// off to on, so it never brings back the entries of a session that has
// ended, and the readers on other goroutines see either answer whole.
func (d *daReplies) applyLate(reply []byte) {
	key := unwrapSession(d.session)
	sixel := sixelFromReplies(d.ext, d.primary)
	audio := audioFromReplies(d.libsndfile, d.wavU8)
	if sixel {
		sessionSixelCaps.CompareAndSwap(key, false, true)
	}
	if audio {
		sessionAudioCaps.CompareAndSwap(key, false, true)
	}
	d.log.Info("late terminal reply", "reply", escapeReply(reply), "sixel", sixel, "audio", audio)
}

// probeModeName names an encoding for the log.
func probeModeName(mode ansi.OutputMode) string {
	switch mode {
	case ansi.OutputModeUTF8:
		return "utf8"
	case ansi.OutputModeCP437:
		return "cp437"
	}
	return "not probed"
}

// encodingFromReport maps the cursor position report after the encoding
// probe to an encoding: one column of movement on the probe's row is UTF-8.
// Two or three, no report, or a report that is malformed, on another row or
// shows any other movement, is CP437, which keeps plain text readable on
// either kind of terminal.
func encodingFromReport(cpr []byte) ansi.OutputMode {
	if row, col, ok := ansi.ParseCPR(cpr); ok && row == ansi.EncodingProbeRow && ansi.EncodingFromProbeColumn(col) == ansi.OutputModeUTF8 {
		return ansi.OutputModeUTF8
	}
	return ansi.OutputModeCP437
}

// audioFromReplies reports whether the terminal said both that libsndfile
// is present and that it plays WAV with 8-bit unsigned PCM. A missing or
// negative answer to either means no. The terminal's name and reported
// version are not consulted.
func audioFromReplies(libsndfile, wavU8 []byte) bool {
	return string(libsndfile) == ansi.AudioLibsndfileYes && string(wavU8) == ansi.AudioWAVU8Yes
}

// escapeReply renders a reply for the log with its control bytes escaped,
// or "none" when there was no reply.
func escapeReply(reply []byte) string {
	if len(reply) == 0 {
		return "none"
	}
	q := strconv.QuoteToASCII(string(reply))
	return q[1 : len(q)-1]
}

// ProbeTerminal asks the caller's terminal, at connect, what it can do. It
// sends the encoding probe when encoding is true, then the extended device
// attributes query, the audio queries and the primary device attributes
// query, in one write. It waits up to timeout in all, and stops when the
// primary reply arrives: a terminal answers in order, so the cursor report,
// if it sends one, is in by then.
//
// With encoding true it returns the encoding the cursor report shows (see
// encodingFromReport): UTF-8 for one column of movement, CP437 otherwise,
// including when no report came. With encoding false it returns
// OutputModeAuto. Whether the terminal draws sixel is stored on the session
// for sessionSixel, and whether it plays the board's audio format for
// sessionAudio; no reply before the deadline means it does not, until a
// late answer says it does. The
// terminal's name plays no part. Replies are logged to log.
//
// It runs before sessionHandler reads from the session, and stops the input
// handler it reads through. Keys typed during the probe are dropped when
// the encoding probe ran, as they always were, and kept for the next reader
// otherwise. Replies are taken out of the session's input before any read
// (see editor.ReplyTrap): device attributes and audio replies for the rest
// of the session, and the one cursor report until it comes or replyWindow
// passes. A late reply never reaches a prompt. A sixel or audio answer
// that comes after the wait but within replyWindow turns that capability
// on for the rest of the session; a late cursor report does not change the
// encoding.
func ProbeTerminal(s ssh.Session, timeout time.Duration, encoding bool, log *slog.Logger) ansi.OutputMode {
	deadline := time.Now().Add(timeout)
	replies := &daReplies{log: log, session: s, lateUntil: time.Now().Add(replyWindow), encoding: encoding,
		answered: make(chan struct{})}
	trap := editor.NewReplyTrap(replyWindow, replies.add)
	replies.trap = trap
	query := ansi.DAExtendedQuery + ansi.AudioQueries + ansi.DAPrimaryQuery
	if encoding {
		trap.ExpectCursorReport(replyWindow, probeReport)
		query = ansi.EncodingProbe + query
	}
	sessionReplyTraps.Store(s, trap)

	getSessionIH(s)               // reads the session, through the trap, while the probe waits
	_, _ = s.Write([]byte(query)) // best-effort probe

	if wait := time.Until(deadline); wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-replies.answered:
		case <-timer.C:
		}
		timer.Stop()
	}
	mode := replies.decide()
	if isClosed(replies.answered) {
		trap.StopExpecting()
	}
	if encoding {
		dropSessionIH(s)
	} else {
		handOffSessionIH(s)
	}
	clear := ansi.ProbeLineClear
	if encoding {
		clear = ansi.EncodingProbeClear
	}
	_, _ = s.Write([]byte(clear)) // best-effort display
	return mode
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// sixelFromReplies reports whether the device attributes replies say the
// terminal draws sixel: the primary reply lists attribute 4, the extended
// reply lists feature 4, or the primary reply is CTerm's at revision 1.189
// or later, which draws sixel before it could list the feature. A parameter
// counts only whole, so 14 or 42 is not 4.
func sixelFromReplies(ext, primary []byte) bool {
	// CTerm's reply starts with 0, the Ps it was asked with.
	if params, ok := daParams(ext, '<'); ok && len(params) > 1 && params[0] == "0" && hasParam(params[1:], "4") {
		return true
	}
	// The first parameter of a DEC reply is the terminal's class (62 is a
	// VT220, 4 a VT132); the attributes follow it.
	if params, ok := daParams(primary, '?'); ok && len(params) > 1 && hasParam(params[1:], "4") {
		return true
	}
	if params, ok := daParams(primary, '='); ok {
		return ctermAtLeast(params, 1, 189)
	}
	return false
}

// daParams splits a reply of the form CSI marker params c into its
// parameters.
func daParams(reply []byte, marker byte) ([]string, bool) {
	if len(reply) < 4 || reply[0] != 0x1b || reply[1] != '[' || reply[2] != marker || reply[len(reply)-1] != 'c' {
		return nil, false
	}
	var params []string
	for _, p := range bytes.Split(reply[3:len(reply)-1], []byte{';'}) {
		params = append(params, string(p))
	}
	return params, true
}

func hasParam(params []string, want string) bool {
	for _, p := range params {
		if p == want {
			return true
		}
	}
	return false
}

// ctermPrefix is "CTerm" as the decimal parameters CTerm's DA reply starts with.
var ctermPrefix = []string{"67", "84", "101", "114", "109"}

// ctermAtLeast reports whether params are a CTerm DA reply for revision
// major.minor or later.
func ctermAtLeast(params []string, major, minor int) bool {
	if len(params) < len(ctermPrefix)+1 {
		return false
	}
	for i, p := range ctermPrefix {
		if params[i] != p {
			return false
		}
	}
	maj, err := strconv.Atoi(params[len(ctermPrefix)])
	if err != nil {
		return false
	}
	minr := 0
	if len(params) > len(ctermPrefix)+1 {
		if minr, err = strconv.Atoi(params[len(ctermPrefix)+1]); err != nil {
			return false
		}
	}
	return maj > major || (maj == major && minr >= minor)
}

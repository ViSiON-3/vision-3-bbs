package acs

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/timeleft"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Session is what the session terms read from a caller's connection. The BBS
// adapts its telnet and SSH sessions to it; a check made outside a call
// (default areas at login, QWK API uploads, menu art) has none.
type Session interface {
	// RemoteAddr is the connection's remote address, for L.
	RemoteAddr() net.Addr
	// HasPTY reports whether the connection has a terminal, for A.
	HasPTY() bool
	// ChatCredit is the sysop chat time credited back to the call, for T.
	ChatCredit() time.Duration
}

// Context is what an expression is evaluated against.
type Context struct {
	// User is the caller. Check fails every non-empty string other than "*"
	// for a nil User; Eval needs a non-nil one.
	User *user.User
	// Session is the caller's connection, nil when there is none: L and A
	// are then false and T counts no chat credit.
	Session Session
	// Start is when the call began, for T.
	Start time.Time
	// Now, when set, is the time H, W, Y and T use; zero means the current
	// time, read when each term is evaluated.
	Now time.Time
	// SysOpLevel and CoSysOpLevel are the thresholds for SYSOP and COSYSOP
	// and the level from which T always passes (sysOpLevel and coSysOpLevel
	// in config.json).
	SysOpLevel, CoSysOpLevel int
}

func (c Context) now() time.Time {
	if c.Now.IsZero() {
		return time.Now()
	}
	return c.Now
}

// Check evaluates the ACS string s against ctx: true when it passes. An empty
// string and a lone "*" pass for everyone, including a nil User; anything
// else fails for a nil User. A string that can't be read as a whole fails.
func Check(s string, ctx Context) bool {
	if s == "" || s == "*" {
		return true
	}
	if ctx.User == nil {
		return false
	}
	u := ctx.User
	slog.Debug("checking ACS", "acs", s, "handle", u.Handle, "level", u.AccessLevel, "flags", u.Flags, "id", u.ID, "validated", u.Validated)
	e := Parse(s)
	if !e.Valid() {
		for _, d := range e.Diagnostics {
			if d.Severity == Error {
				slog.Error("invalid ACS string", "acs", s, "error", d.String())
			}
		}
		return false
	}
	result := e.Eval(ctx)
	slog.Debug("ACS check result", "acs", s, "result", result)
	return result
}

// Eval evaluates a valid expression against ctx; it is false for an
// expression that isn't Valid. ctx.User must not be nil. Every term is
// evaluated, as the BBS always has, so each logs its own check.
func (e *Expr) Eval(ctx Context) bool {
	if e.Root == nil {
		return false
	}
	return evalNode(e.Root, ctx)
}

func evalNode(n *Node, ctx Context) bool {
	switch n.Kind {
	case TermNode:
		return EvalTerm(n.Text, ctx)
	case NotNode:
		return !evalNode(n.X, ctx)
	case AndNode:
		x, y := evalNode(n.X, ctx), evalNode(n.Y, ctx)
		return x && y
	default:
		x, y := evalNode(n.X, ctx), evalNode(n.Y, ctx)
		return x || y
	}
}

// termProblem says why a term is always false (an unknown letter or a bad
// argument), or "" for a term that is evaluated. It follows EvalTerm: every
// term it accepts, EvalTerm evaluates, and every term it rejects, EvalTerm
// treats as false.
func termProblem(term string) string {
	code, value, keyword := splitTerm(term)
	if keyword {
		return ""
	}
	number := func() string {
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Sprintf("%q: %s needs a whole number after it", term, code)
		}
		return ""
	}
	switch code {
	case "S", "D", "U", "P", "E", "B", "T", "C", "X":
		return number()
	case "H":
		if h, err := strconv.Atoi(value); err != nil || h < 0 || h > 23 {
			return fmt.Sprintf("%q: H needs an hour from 0 to 23", term)
		}
	case "W":
		if d, err := strconv.Atoi(value); err != nil || d < 0 || d > 6 {
			return fmt.Sprintf("%q: W needs a weekday from 0 (Sunday) to 6", term)
		}
	case "F":
		if len(value) != 1 {
			return fmt.Sprintf("%q: F takes exactly one flag letter or digit (use FA&FB for two)", term)
		}
	case "Y":
		parts := strings.Split(value, "/")
		if len(parts) != 2 {
			return fmt.Sprintf("%q: Y needs a range written hh:mm/hh:mm", term)
		}
		if _, err := time.Parse("15:04", parts[0]); err != nil {
			return fmt.Sprintf("%q: Y needs a range written hh:mm/hh:mm", term)
		}
		if _, err := time.Parse("15:04", parts[1]); err != nil {
			return fmt.Sprintf("%q: Y needs a range written hh:mm/hh:mm", term)
		}
	case "L", "A", "V", "I", "Z":
	default:
		return fmt.Sprintf("%q: %s is not an ACS term", term, code)
	}
	return ""
}

// EvalTerm evaluates a single term, such as S50 or FA, against ctx, whose
// User must not be nil. A term with an unknown letter or a bad argument is
// false and logged as a warning.
func EvalTerm(condition string, ctx Context) bool {
	if len(condition) == 0 {
		slog.Warn("empty condition encountered in ACS string after tokenization")
		return false
	}
	u := ctx.User
	s := ctx.Session

	code, value, keyword := splitTerm(condition)
	if keyword {
		if code == "SYSOP" {
			return u.AccessLevel >= ctx.SysOpLevel
		}
		return u.AccessLevel >= ctx.CoSysOpLevel
	}

	var result bool
	switch code {
	case "S": // Access level >= value
		level, err := strconv.Atoi(value)
		if err != nil {
			slog.Warn("invalid level value in ACS condition", "condition", condition, "error", err)
			result = false
		} else {
			result = u.AccessLevel >= level
		}
	case "L": // Local connection
		if s == nil {
			result = false
		} else {
			network := s.RemoteAddr().Network()
			addr := s.RemoteAddr().String()
			isLoopback := strings.HasPrefix(addr, "127.") || strings.HasPrefix(addr, "[::1]")
			result = (network == "pipe" || network == "unix" || isLoopback)
			slog.Debug("ACS 'L' check", "network", network, "addr", addr, "isLoopback", isLoopback, "result", result)
		}
	case "A": // Session has a terminal: every telnet session, SSH with a PTY. Not an ANSI or encoding check.
		if s == nil {
			result = false
		} else {
			isPty := s.HasPTY()
			result = isPty
			slog.Debug("ACS 'A' check", "isPty", isPty, "result", result)
		}
	case "F": // Flag is set
		if len(value) != 1 {
			slog.Warn("invalid flag value in ACS condition: flag must be single character", "condition", condition)
			result = false
		} else {
			result = strings.Contains(strings.ToUpper(u.Flags), strings.ToUpper(value))
		}
	case "U": // User ID == value
		id, err := strconv.Atoi(value)
		if err != nil {
			slog.Warn("invalid user ID value in ACS condition", "condition", condition, "error", err)
			result = false
		} else {
			result = u.ID == id
		}
	case "V": // User is validated
		result = u.Validated
	case "D": // Access level >= value: the same test as S; there is no separate download level
		level, err := strconv.Atoi(value)
		if err != nil {
			slog.Warn("invalid level value in ACS condition", "condition", condition, "error", err)
			result = false
		} else {
			result = u.AccessLevel >= level
		}
	case "E": // Uploads per call, as a percentage rounded down, >= value (uploads, not posts)
		pcrThreshold, err := strconv.Atoi(value)
		if err != nil {
			slog.Warn("invalid PCR value in ACS condition", "condition", condition, "error", err)
			result = false
		} else {
			numLogons := u.TimesCalled
			if numLogons <= 0 {
				result = false
			} else {
				userPCR := (float64(u.NumUploads) / float64(numLogons)) * 100
				result = int(userPCR) >= pcrThreshold
				slog.Debug("ACS 'E' check", "uploads", u.NumUploads, "calls", numLogons, "pcr", userPCR, "threshold", pcrThreshold, "result", result)
			}
		}
	case "H": // Current Hour == value
		hour, err := strconv.Atoi(value)
		if err != nil || hour < 0 || hour > 23 {
			slog.Warn("invalid hour value in ACS condition", "condition", condition, "error", err)
			result = false
		} else {
			result = ctx.now().Hour() == hour
		}
	case "P": // File Points >= value
		points, err := strconv.Atoi(value)
		if err != nil {
			slog.Warn("invalid points value in ACS condition", "condition", condition, "error", err)
			result = false
		} else {
			result = u.FilePoints >= points
		}
	case "T": // Time Left >= value (minutes)
		minutesLeftThreshold, err := strconv.Atoi(value)
		if err != nil {
			slog.Warn("invalid time left value in ACS condition", "condition", condition, "error", err)
			result = false
		} else {
			// CoSysOps and above have no time limit (MenuExecutor.timeLimit).
			if u.TimeLimit <= 0 || u.AccessLevel >= ctx.CoSysOpLevel {
				slog.Debug("ACS 'T' check: user has no time limit, passing", "timeLimit", u.TimeLimit, "level", u.AccessLevel)
				result = true
			} else {
				var credit time.Duration
				if s != nil {
					credit = s.ChatCredit()
				}
				left, _ := timeleft.Remaining(u.TimeLimit, ctx.Start, ctx.now(), credit)
				threshold := time.Duration(minutesLeftThreshold) * time.Minute
				result = left >= threshold
				slog.Debug("ACS 'T' check", "limitMin", u.TimeLimit, "left", left, "threshold", threshold, "result", result)
			}
		}
	case "W": // Day of Week == value (0=Sun, 1=Mon, ... 6=Sat)
		day, err := strconv.Atoi(value)
		if err != nil || day < 0 || day > 6 {
			slog.Warn("invalid day of week value in ACS condition", "condition", condition, "error", err)
			result = false
		} else {
			result = int(ctx.now().Weekday()) == day
		}
	case "Z": // String exists in PrivateNote
		result = strings.Contains(strings.ToUpper(u.PrivateNote), strings.ToUpper(value))
	case "Y": // Within Time Range hh:mm/hh:mm
		parts := strings.Split(value, "/")
		if len(parts) != 2 {
			slog.Warn("invalid time range format in ACS condition: expected hh:mm/hh:mm", "condition", condition)
			result = false
		} else {
			now := ctx.now()
			layout := "15:04"
			startTime, errStart := time.Parse(layout, parts[0])
			endTime, errEnd := time.Parse(layout, parts[1])
			if errStart != nil || errEnd != nil {
				slog.Warn("invalid time format in ACS time range", "value", value, "startError", errStart, "endError", errEnd)
				result = false
			} else {
				result = InTimeRange(now, startTime, endTime)
				slog.Debug("ACS 'Y' check", "range", value, "now", now, "result", result)
			}
		}
	case "B": // Upload/Download ratio >= value (as percentage)
		ratioThreshold, err := strconv.Atoi(value)
		if err != nil {
			slog.Warn("invalid ratio value in ACS condition", "condition", condition, "error", err)
			result = false
		} else {
			if u.NumDownloads <= 0 {
				// No downloads: ratio is infinite (always passes)
				result = true
			} else {
				ratio := (float64(u.NumUploads) / float64(u.NumDownloads)) * 100
				result = int(ratio) >= ratioThreshold
			}
			slog.Debug("ACS 'B' check", "uploads", u.NumUploads, "downloads", u.NumDownloads, "threshold", ratioThreshold, "result", result)
		}
	case "C": // Message Conference == value
		confID, err := strconv.Atoi(value)
		if err != nil {
			slog.Warn("invalid conference ID in ACS condition", "condition", condition, "error", err)
			result = false
		} else {
			result = u.CurrentMsgConferenceID == confID
		}
	case "I": // User has flag I: the same test as FI; any argument is ignored
		result = strings.Contains(strings.ToUpper(u.Flags), "I")
		slog.Debug("ACS 'I' check", "flags", u.Flags, "result", result)
	case "X": // File Conference == value
		confID, err := strconv.Atoi(value)
		if err != nil {
			slog.Warn("invalid conference ID in ACS condition", "condition", condition, "error", err)
			result = false
		} else {
			result = u.CurrentFileConferenceID == confID
		}
	default:
		slog.Warn("unknown ACS code in condition", "code", code, "condition", condition)
		result = false
	}
	return result
}

// InTimeRange reports whether now's clock time falls within start-end, both
// minutes included: 00:00/23:59 is the whole day, through 23:59:59. A range
// whose end is before its start runs past midnight. Only the hours and
// minutes of each time are used.
func InTimeRange(now, start, end time.Time) bool {
	m := now.Hour()*60 + now.Minute()
	from := start.Hour()*60 + start.Minute()
	to := end.Hour()*60 + end.Minute()
	if to < from {
		return m >= from || m <= to
	}
	return m >= from && m <= to
}

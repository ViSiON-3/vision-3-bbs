package acs

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/timeleft"
)

// A term is a letter with its argument (S20, FA, Y22:00/06:00) or a readable
// name for the same test (LEVEL>=20, FLAG:A, TIME:22:00/06:00). The numeric
// terms also take a comparison (S<20, H=8) or a range (S20-99, H22-6), and
// both forms read into the same term, so a name means exactly what its
// letter does.

// parsedTerm is a term read by parseTerm.
type parsedTerm struct {
	// code is the letter, upper case, or SYSOP or COSYSOP.
	code string
	// op is the comparison for a numeric term: "" for the letter's own
	// meaning (S20 is level >= 20, H8 is hour = 8), or = < <= > >=.
	op string
	// lo and hi are a numeric term's number, or its range's ends (hi is lo
	// for a single number). A range whose low end is above its high end runs
	// past the top (H22-6, W5-1); only the clock and calendar terms allow it.
	lo, hi int
	ranged bool
	// value is the argument of F, Z and Y.
	value string
}

// termName is a readable name: the letter it stands for and how it takes its
// argument.
type termName struct {
	code string
	form nameForm
}

type nameForm int

const (
	nameCompare nameForm = iota // LEVEL>=20, HOUR=8-17
	nameColon                   // FLAG:A, NOTE:text, TIME:22:00/06:00
	nameAlone                   // VALIDATED
)

// termNames are the readable names, one per letter that has one. D (the same
// test as S) and I (the same test as FI) have none.
var termNames = map[string]termName{
	"LEVEL":          {"S", nameCompare},
	"USER":           {"U", nameCompare},
	"FILEPOINTS":     {"P", nameCompare},
	"UPLOADSPERCALL": {"E", nameCompare},
	"ULDLRATIO":      {"B", nameCompare},
	"TIMELEFT":       {"T", nameCompare},
	"HOUR":           {"H", nameCompare},
	"WEEKDAY":        {"W", nameCompare},
	"MSGCONF":        {"C", nameCompare},
	"FILECONF":       {"X", nameCompare},
	"FLAG":           {"F", nameColon},
	"NOTE":           {"Z", nameColon},
	"TIME":           {"Y", nameColon},
	"VALIDATED":      {"V", nameAlone},
	"LOCAL":          {"L", nameAlone},
	"TERMINAL":       {"A", nameAlone},
	// Values from the user's record, with no letter of their own.
	"CALLS":     {"CALLS", nameCompare},
	"POSTS":     {"POSTS", nameCompare},
	"UPLOADS":   {"UPLOADS", nameCompare},
	"DOWNLOADS": {"DOWNLOADS", nameCompare},
	"AGE":       {"AGE", nameCompare},
	"REALNAME":  {"REALNAME", nameAlone},
}

// numericCodes are the terms that take a number, with what a bare number
// means: at least (S20 is level >= 20) or exactly (U5 is user 5). The named
// counts always take a comparison, so theirs is never used.
var numericCodes = map[string]string{
	"S": ">=", "D": ">=", "P": ">=", "E": ">=", "B": ">=", "T": ">=",
	"U": "=", "H": "=", "W": "=", "C": "=", "X": "=",
	"CALLS": ">=", "POSTS": ">=", "UPLOADS": ">=", "DOWNLOADS": ">=", "AGE": ">=",
}

// cyclic codes are the ones whose ranges may run past the top: H22-6 is
// 22:00 to 06:59, W5-1 Friday to Monday.
func cyclic(code string) bool { return code == "H" || code == "W" }

// isCompareRune reports whether r is part of a comparison or a range. The
// tokenizer takes them into a term only between term characters; anywhere
// else they are unexpected characters.
func isCompareRune(r rune) bool { return r == '<' || r == '>' || r == '=' || r == '-' }

// parseTerm reads a term. problem says why it can't be evaluated, "" for a
// term that can; a string with such a term fails as a whole.
func parseTerm(text string) (t parsedTerm, problem string) {
	upper := strings.ToUpper(text)
	if upper == "SYSOP" || upper == "COSYSOP" {
		return parsedTerm{code: upper}, ""
	}
	compares := strings.ContainsFunc(text, isCompareRune)

	// A readable name. A name that isn't followed by what it takes reads as
	// the letter it starts with, as it always has: LEVEL alone is L with an
	// argument, not a level test.
	word := strings.ToUpper(text[:wordLen(text)])
	rest := text[len(word):]
	if n, ok := termNames[word]; ok {
		switch {
		case n.form == nameAlone && rest == "":
			return parsedTerm{code: n.code}, ""
		case n.form == nameColon && strings.HasPrefix(rest, ":"):
			return readValue(text, word, n.code, rest[1:])
		case n.form == nameCompare && rest != "" && strings.ContainsRune("<>=", rune(rest[0])):
			return readNumber(text, word, n.code, rest)
		case n.form == nameAlone && rest != "" && compares:
			return parsedTerm{}, fmt.Sprintf("%q: %s takes nothing after it", text, word)
		}
	}

	code, value := strings.ToUpper(text[:1]), text[1:]
	if _, ok := numericCodes[code]; ok {
		return readNumber(text, code, code, value)
	}
	switch code {
	case "F", "Z", "Y":
		return readValue(text, code, code, value)
	case "L", "A", "V", "I":
		// The argument is ignored, as it always has been.
		if compares {
			return parsedTerm{}, fmt.Sprintf("%q: %s takes no comparison", text, code)
		}
		return parsedTerm{code: code}, ""
	}
	return parsedTerm{}, fmt.Sprintf("%q: %s is not an ACS term", text, code)
}

// wordLen is the length of text's leading run of letters.
func wordLen(text string) int {
	for i, r := range text {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return i
		}
	}
	return len(text)
}

// readNumber reads a numeric term's comparison and number or range. name is
// how the term was written (S or LEVEL), for messages.
func readNumber(text, name, code, arg string) (parsedTerm, string) {
	t := parsedTerm{code: code}
	for _, op := range []string{"<=", ">=", "<", ">", "="} {
		if strings.HasPrefix(arg, op) {
			t.op, arg = op, arg[len(op):]
			break
		}
	}
	bad := func(msg string) (parsedTerm, string) {
		return parsedTerm{}, fmt.Sprintf("%q: %s", text, msg)
	}
	if arg != "" && strings.ContainsRune("<>=", rune(arg[0])) {
		return bad("the comparison is one of = < <= > >=")
	}
	lo, hi, ranged := arg, arg, false
	if i := strings.IndexByte(arg, '-'); i >= 0 {
		lo, hi, ranged = arg[:i], arg[i+1:], true
	}
	var err1, err2 error
	t.lo, err1 = strconv.Atoi(lo)
	t.hi, err2 = strconv.Atoi(hi)
	if err1 != nil || err2 != nil || strings.HasPrefix(lo, "+") || strings.HasPrefix(hi, "+") {
		if ranged {
			return bad(name + " needs a range of whole numbers, low-high")
		}
		return bad(name + " needs a whole number after it")
	}
	t.ranged = ranged
	if ranged && t.op != "" && t.op != "=" {
		return bad("a range takes = or no comparison, not " + t.op)
	}
	switch code {
	case "H":
		if t.lo < 0 || t.lo > 23 || t.hi < 0 || t.hi > 23 {
			return bad(name + " needs an hour from 0 to 23")
		}
	case "W":
		if t.lo < 0 || t.lo > 6 || t.hi < 0 || t.hi > 6 {
			return bad(name + " needs a weekday from 0 (Sunday) to 6")
		}
	}
	if ranged && t.lo > t.hi && !cyclic(code) {
		return bad(fmt.Sprintf("a %s range runs from low to high (%d-%d); only the hour and weekday wrap around", name, t.hi, t.lo))
	}
	return t, ""
}

// readValue reads the argument of F, Z and Y, or of their names.
func readValue(text, name, code, arg string) (parsedTerm, string) {
	bad := func(msg string) (parsedTerm, string) {
		return parsedTerm{}, fmt.Sprintf("%q: %s", text, msg)
	}
	switch code {
	case "F":
		if len(arg) != 1 || isCompareRune(rune(arg[0])) {
			if name == "F" {
				return bad("F takes exactly one flag letter or digit (use FA&FB for two)")
			}
			return bad(name + ": takes exactly one flag letter or digit (use FLAG:A&FLAG:B for two)")
		}
	case "Z":
		if strings.ContainsAny(arg, "<>=") {
			return bad(name + " takes text to look for, not a comparison")
		}
		if name != "Z" && arg == "" {
			return bad(name + ": needs the text to look for")
		}
	case "Y":
		parts := strings.Split(arg, "/")
		ok := len(parts) == 2
		for _, p := range parts {
			if _, err := time.Parse("15:04", p); err != nil {
				ok = false
			}
		}
		if !ok {
			return bad(name + " needs a range written hh:mm/hh:mm")
		}
	}
	return parsedTerm{code: code, value: arg}, ""
}

// measured is a caller's value for a numeric term.
type measured struct {
	have int
	// unlimited: there is no limit to compare (B with no downloads, T with
	// no time limit), which is above every number.
	unlimited bool
	// none: there is no value (E before the first call), so the term fails.
	none bool
}

// measure reads the caller's value for a numeric term.
func measure(t parsedTerm, ctx Context) measured {
	u := ctx.User
	switch t.code {
	case "S", "D":
		return measured{have: u.AccessLevel}
	case "U":
		return measured{have: u.ID}
	case "P":
		return measured{have: u.FilePoints}
	case "E": // uploads per call, as a percentage rounded down
		if u.TimesCalled <= 0 {
			return measured{none: true}
		}
		return measured{have: int(float64(u.NumUploads) / float64(u.TimesCalled) * 100)}
	case "B": // uploads per download, as a percentage rounded down
		if u.NumDownloads <= 0 {
			return measured{unlimited: true}
		}
		return measured{have: int(float64(u.NumUploads) / float64(u.NumDownloads) * 100)}
	case "T": // minutes left; co-sysops and up have no time limit (MenuExecutor.timeLimit)
		if u.TimeLimit <= 0 || u.AccessLevel >= ctx.CoSysOpLevel {
			return measured{unlimited: true}
		}
		var credit time.Duration
		if ctx.Session != nil {
			credit = ctx.Session.ChatCredit()
		}
		left, _ := timeleft.Remaining(u.TimeLimit, ctx.Start, ctx.now(), credit)
		// left is never negative, so whole minutes compare with a number
		// exactly as the duration does.
		return measured{have: int(left / time.Minute)}
	case "H":
		return measured{have: ctx.now().Hour()}
	case "W":
		return measured{have: int(ctx.now().Weekday())}
	case "C":
		return measured{have: u.CurrentMsgConferenceID}
	case "X":
		return measured{have: u.CurrentFileConferenceID}
	case "CALLS":
		return measured{have: u.TimesCalled}
	case "POSTS":
		return measured{have: u.MessagesPosted}
	case "UPLOADS":
		return measured{have: u.NumUploads}
	case "DOWNLOADS":
		return measured{have: u.NumDownloads}
	case "AGE":
		if u.CreatedAt.IsZero() { // lacking stops the evaluation first
			return measured{none: true}
		}
		return measured{have: AccountAge(u.CreatedAt, ctx.now(), ctx.location())}
	}
	return measured{none: true}
}

// AccountAge is how many whole calendar days lie between created and now in
// loc: 0 on the day of sign-up, 1 from the next midnight. A creation time
// after now counts as 0.
func AccountAge(created, now time.Time, loc *time.Location) int {
	day := func(t time.Time) time.Time {
		y, m, d := t.In(loc).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	return max(int(day(now).Sub(day(created))/(24*time.Hour)), 0)
}

// compare decides a numeric term for a measured value.
func (t parsedTerm) compare(m measured) bool {
	if m.none {
		return false
	}
	op := t.op
	if op == "" && !t.ranged {
		op = numericCodes[t.code]
	}
	if m.unlimited {
		return !t.ranged && (op == ">=" || op == ">")
	}
	have := m.have
	if t.ranged {
		if t.lo <= t.hi {
			return have >= t.lo && have <= t.hi
		}
		return have >= t.lo || have <= t.hi
	}
	switch op {
	case "<":
		return have < t.lo
	case "<=":
		return have <= t.lo
	case ">":
		return have > t.lo
	case ">=":
		return have >= t.lo
	}
	return have == t.lo
}

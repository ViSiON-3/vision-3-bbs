package msgsearch

import (
	"errors"
	"strings"
	"testing"
)

func TestParseAndMatch(t *testing.T) {
	const subject = "Blue Box plans"
	const body = "How the TSPS operator\nservices console worked.\r\nNo 2600 Hz here, just MF."
	tests := []struct {
		query string
		want  bool
	}{
		{"blue", true},
		{"BLUE", true},                 // case-insensitive
		{"box", true},                  // substring of a word
		{"boxes", false},               //
		{"tsps", true},                 // body
		{"plans tsps", true},           // adjacent terms are ANDed, across subject and body
		{"plans AND red", false},       //
		{"red OR tsps", true},          //
		{"NOT red", true},              //
		{"NOT blue", false},            //
		{`"blue box"`, true},           // phrase
		{`"box blue"`, false},          // phrase order matters
		{`"operator services"`, true},  // a phrase matches across a line break
		{`"plans how"`, false},         // a phrase never spans subject and body
		{"red OR blue AND tsps", true}, // AND before OR: red OR (blue AND tsps)
		{"(red OR blue) AND 2600", true},
		{"(red OR green) AND 2600", false},
		{"blue AND NOT 2600", false}, // NOT before AND
		{"NOT (red OR green)", true}, //
		{"NOT NOT blue", true},       //
		{"red or tsps", true},        // operators in any case
		{`"and"`, false},             // a quoted operator is a word: no "and" in the text
		{`"here, just"`, true},       // punctuation is part of the text
		{"\x1b[1mblue\x1b[0m", true}, // escape codes in the query are dropped
		{"  blue  ", true},
	}
	for _, tt := range tests {
		q, err := Parse(tt.query)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.query, err)
			continue
		}
		if got := q.Match(subject, body); got != tt.want {
			t.Errorf("Parse(%q).Match = %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestMatchIgnoresANSIInText(t *testing.T) {
	q, err := Parse(`"blue box"`)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Match("\x1b[1;34mBlue\x1b[0m Box") {
		t.Error("ANSI codes between the words stopped the phrase matching")
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		query string
		pos   int
		msg   string
	}{
		{"", 0, "empty search"},
		{"   ", 0, "empty search"},
		{"(blue", 1, "unmatched ("},
		{"blue)", 5, "unmatched )"},
		{"((blue)", 1, "unmatched ("},
		{"()", 1, "empty ( )"},
		{"blue AND", 9, "search ends with an operator"},
		{"blue OR", 8, "search ends with an operator"},
		{"NOT", 4, "search ends with an operator"},
		{"AND blue", 1, "missing search term before AND"},
		{"OR blue", 1, "missing search term before OR"},
		{"blue AND OR red", 10, "missing search term before OR"},
		{"(OR blue)", 2, "missing search term before OR"},
		{`"blue box`, 1, "unterminated quote"},
		{`""`, 1, "empty phrase"},
		{`"   "`, 1, "empty phrase"},
		{strings.Repeat("a", MaxQueryLen+1), MaxQueryLen + 1, "search longer than 80 characters"},
	}
	for _, tt := range tests {
		_, err := Parse(tt.query)
		var qe *Error
		if !errors.As(err, &qe) {
			t.Errorf("Parse(%q) error = %v, want an *Error", tt.query, err)
			continue
		}
		if qe.Pos != tt.pos || qe.Msg != tt.msg {
			t.Errorf("Parse(%q) = %q at %d, want %q at %d", tt.query, qe.Msg, qe.Pos, tt.msg, tt.pos)
		}
	}
}

func TestErrorString(t *testing.T) {
	if got := (&Error{Pos: 5, Msg: "unmatched ("}).Error(); got != "unmatched ( at 5" {
		t.Errorf("got %q", got)
	}
	if got := (&Error{Msg: "empty search"}).Error(); got != "empty search" {
		t.Errorf("got %q", got)
	}
}

func TestMaxLengthAccepted(t *testing.T) {
	if _, err := Parse(strings.Repeat("a", MaxQueryLen)); err != nil {
		t.Errorf("a %d-character query: %v", MaxQueryLen, err)
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"  Hello\r\n\tWorld  ": "hello world",
		"\x1b[1;31mRed\x1b[0m": "red",
		"a\x1bb":               "ab",
		"blue|15 box|07":       "blue box",
		"|B1|12Red|B10 ok":     "red ok",
		"a||15b":               "a||15b",
		"|99 and |":            "|99 and |",
		"":                     "",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// Colour pipe codes in a message are ignored: a phrase matches across one,
// and the digits of a code are not text.
func TestMatchIgnoresPipeCodes(t *testing.T) {
	const body = "Plans for a |09blue|15 box|07, rev 2."
	for query, want := range map[string]bool{
		`"blue box"`: true,
		"bluebox":    false,
		"15":         false,
		"09":         false,
		"rev 2":      true,
	} {
		q, err := Parse(query)
		if err != nil {
			t.Fatalf("Parse(%q): %v", query, err)
		}
		if got := q.Match("", body); got != want {
			t.Errorf("%s: Match = %v, want %v", query, got, want)
		}
	}
}

func TestString(t *testing.T) {
	q, err := Parse("  blue box ")
	if err != nil {
		t.Fatal(err)
	}
	if q.String() != "blue box" {
		t.Errorf("String() = %q", q.String())
	}
}

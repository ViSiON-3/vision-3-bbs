package message

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

func TestMessageKludgeLines(t *testing.T) {
	sf := func(id uint16, v string) jam.Subfield { return jam.Subfield{LoID: id, Buffer: []byte(v)} }
	msg := &jam.Message{
		Header: &jam.MessageHeader{
			DateWritten: 1759500000,
			Attribute:   jam.MsgTypeEcho | jam.MsgSent,
			Subfields: []jam.Subfield{
				sf(jam.SfldOAddress, "1:340/1101"),
				sf(jam.SfldPath2D, "340/1101 400 128/187"),
				sf(jam.SfldSeenBy2D, "633/280 2744"),
				sf(jam.SfldFTSKludge, "TZUTC: -0400"),
				sf(jam.SfldMsgID, "1:340/1101 62b9873f"),
				sf(jam.SfldPID, "Elereader"),
			},
		},
		Text: "Hello\r\x01TID: SBBSecho 3.37\r--- tear\rSEEN-BY: 1/2 3\r",
	}
	got := messageKludgeLines(msg)
	joined := strings.Join(got, "\n")

	for _, want := range []string{
		"Orig address: 1:340/1101",
		"Attributes: 01000010 Sent Echo",
		"MSGID: 1:340/1101 62b9873f",
		"PID: Elereader",
		"TZUTC: -0400",
		"TID: SBBSecho 3.37", // body kludge, ^A dropped
		"SEEN-BY: 633/280 2744",
		"SEEN-BY: 1/2 3",
		"PATH: 340/1101 400 128/187",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if strings.ContainsRune(joined, '\x01') {
		t.Error("control character reached the output")
	}
	// Wire order: MSGID before the other kludges, SEEN-BY before PATH last.
	order := []string{"MSGID:", "TZUTC:", "SEEN-BY:", "PATH:"}
	for i := 1; i < len(order); i++ {
		if strings.Index(joined, order[i-1]) > strings.Index(joined, order[i]) {
			t.Errorf("%s comes after %s:\n%s", order[i-1], order[i], joined)
		}
	}
}

// Body kludges join the same groups as header fields, and values keep their
// raw CP437 bytes.
func TestMessageKludgeLines_BodyGroupingAndCP437(t *testing.T) {
	msg := &jam.Message{
		Header: &jam.MessageHeader{},
		Text: "\x01MSGID: 2:250/1 abc\r\x01PATH: 250/1 153/757\r\x01CHRS: CP437 2\r" +
			"\x01NOTE: Caf\x82\rHi\r--- x\rSEEN-BY: 1/2\rPATH: 3/4\r@PATH: 5/6\r",
	}
	joined := strings.Join(messageKludgeLines(msg), "\n")
	for _, want := range []string{"MSGID: 2:250/1 abc", "PATH: 250/1 153/757", "CHRS: CP437 2",
		"NOTE: Caf\x82", "SEEN-BY: 1/2", "PATH: 3/4", "@PATH: 5/6"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%q", want, joined)
		}
	}
	order := []string{"MSGID:", "CHRS:", "SEEN-BY:", "PATH: 250/1"}
	for i := 1; i < len(order); i++ {
		if strings.Index(joined, order[i-1]) > strings.Index(joined, order[i]) {
			t.Errorf("%s comes after %s:\n%s", order[i-1], order[i], joined)
		}
	}
}

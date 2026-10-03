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
	idx := func(s string) int { return strings.Index(joined, s) }
	if !(idx("MSGID:") < idx("TZUTC:") && idx("TZUTC:") < idx("SEEN-BY:") && idx("SEEN-BY:") < idx("PATH:")) {
		t.Errorf("lines out of order:\n%s", joined)
	}
}

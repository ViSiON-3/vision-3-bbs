package qwkservice

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// messagesDAT returns the MESSAGES.DAT bytes of a packet.
func messagesDAT(t *testing.T, packet []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(packet), int64(len(packet)))
	if err != nil {
		t.Fatalf("packet not a zip: %v", err)
	}
	for _, fl := range zr.File {
		if fl.Name != "MESSAGES.DAT" {
			continue
		}
		rc, err := fl.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	t.Fatal("packet missing MESSAGES.DAT")
	return ""
}

// #856: an offline packet gets message bodies with the sequences a terminal
// would act on taken out, as the reader shows them: device queries, window
// titles and mode changes. Colour and cursor movement stay for readers that
// draw ANSI, and raw CP437 bytes stay as they are, including pairs that would
// read as a UTF-8 control.
func TestBuildPacket_FiltersBodyControls(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.seed(1,
		dm(1, "a", "All", "utf8", "OPEN\x1b[6n\x1b]0;title\x07\x1b[?25l\x1b[1;31mred\x1b[0m\x1b[2;5Hcafé CLOSE"),
		dm(2, "b", "All", "cp437", "CP\xc2\x9b\xdb\x05 END"), // ┬¢█, a raw ENQ
	)
	svc := newTestService(t, store)
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL"}})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	dat := messagesDAT(t, res.Packet)
	for _, bad := range []string{"\x1b[6n", "\x1b]0;", "title", "\x1b[?25l", "\x05"} {
		if strings.Contains(dat, bad) {
			t.Errorf("packet carries %q", bad)
		}
	}
	for _, good := range []string{"\x1b[1;31mred\x1b[0m\x1b[2;5Hcafé CLOSE", "CP\xc2\x9b\xdb END"} {
		if !strings.Contains(dat, good) {
			t.Errorf("packet lost %q", good)
		}
	}
}

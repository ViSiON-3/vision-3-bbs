package jam

import (
	"errors"
	"strings"
	"testing"
)

func TestSetReceiverNameReaddressesMessage(t *testing.T) {
	b, basePath := openCovTestBase(t)
	writeCovMsg(t, b, "Sysop", "All", "First", "first body")
	target := writeCovMsg(t, b, "Sysop", "Old Name", "Second", "second body")
	writeCovMsg(t, b, "Sysop", "All", "Third", "third body")

	before, err := b.ReadMessage(target)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	oldIdx, err := b.ReadIndexRecord(target)
	if err != nil {
		t.Fatalf("ReadIndexRecord: %v", err)
	}
	modBefore, err := b.GetModCounter()
	if err != nil {
		t.Fatalf("GetModCounter: %v", err)
	}

	// A longer name cannot be rewritten in place, so the header must move.
	if err := b.SetReceiverName(target, "A Considerably Longer Name"); err != nil {
		t.Fatalf("SetReceiverName: %v", err)
	}

	after, err := b.ReadMessage(target)
	if err != nil {
		t.Fatalf("ReadMessage after: %v", err)
	}
	if after.To != "A Considerably Longer Name" {
		t.Errorf("To = %q, want the new name", after.To)
	}
	if after.From != before.From || after.Subject != before.Subject || after.Text != before.Text {
		t.Errorf("message changed beyond its receiver: from=%q subject=%q text=%q", after.From, after.Subject, after.Text)
	}
	if after.Header.MessageNumber != before.Header.MessageNumber || after.IsDeleted() {
		t.Errorf("header number=%d deleted=%v, want number %d and not deleted",
			after.Header.MessageNumber, after.IsDeleted(), before.Header.MessageNumber)
	}
	if got := len(after.Header.GetAllSubfieldsByType(SfldReceiverName)); got != 1 {
		t.Errorf("receiver subfields = %d, want 1", got)
	}

	// The index now matches the new recipient, not the old one.
	newIdx, err := b.ReadIndexRecord(target)
	if err != nil {
		t.Fatalf("ReadIndexRecord after: %v", err)
	}
	if want := CRC32String("a considerably longer name"); newIdx.ToCRC != want {
		t.Errorf("index ToCRC = %08x, want %08x", newIdx.ToCRC, want)
	}
	if newIdx.HdrOffset <= oldIdx.HdrOffset {
		t.Errorf("index offset %d not moved past the old header at %d", newIdx.HdrOffset, oldIdx.HdrOffset)
	}
	for name, want := range map[string]int{"A Considerably Longer Name": 1, "Old Name": 0, "All": 2} {
		if got, err := b.CountMessagesToUser(name); err != nil || got != want {
			t.Errorf("CountMessagesToUser(%q) = %d, %v; want %d", name, got, err, want)
		}
	}

	// The superseded header is left behind marked deleted and textless, so
	// nothing walking the .jhr mistakes it for a live message.
	if _, err := b.jhrFile.Seek(int64(oldIdx.HdrOffset), 0); err != nil {
		t.Fatal(err)
	}
	retired, err := b.readHeaderFromReader(b.jhrFile)
	if err != nil {
		t.Fatalf("read retired header: %v", err)
	}
	if retired.Attribute&MsgDeleted == 0 || retired.TxtLen != 0 {
		t.Errorf("retired header attribute=%08x txtlen=%d, want deleted with no text", retired.Attribute, retired.TxtLen)
	}

	if modAfter, _ := b.GetModCounter(); modAfter != modBefore+1 {
		t.Errorf("ModCounter = %d, want %d", modAfter, modBefore+1)
	}
	if n, _ := b.GetMessageCount(); n != 3 {
		t.Errorf("message count = %d, want 3", n)
	}
	if n := b.GetActiveMessageCount(); n != 3 {
		t.Errorf("active message count = %d, want 3", n)
	}

	// The change is on disk, and survives a pack.
	reopened, err := Open(basePath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	res, err := reopened.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if res.MessagesAfter != 3 {
		t.Errorf("messages after pack = %d, want 3", res.MessagesAfter)
	}
	packed, err := reopened.ReadMessage(target)
	if err != nil {
		t.Fatalf("ReadMessage after pack: %v", err)
	}
	if packed.To != "A Considerably Longer Name" || packed.Text != "second body" {
		t.Errorf("after pack: to=%q text=%q", packed.To, packed.Text)
	}
}

func TestSetReceiverNameNormalisesReceiverSubfields(t *testing.T) {
	b, _ := openCovTestBase(t)
	none := writeCovMsg(t, b, "Sysop", "Someone", "No receiver", "body")
	twice := writeCovMsg(t, b, "Sysop", "Someone", "Two receivers", "body")

	// Rewrite the stored headers so one has no receiver subfield and the
	// other has two, as a foreign tosser might leave them.
	rewrite := func(msgNum int, edit func([]Subfield) []Subfield) {
		t.Helper()
		hdr, err := b.ReadMessageHeader(msgNum)
		if err != nil {
			t.Fatalf("ReadMessageHeader(%d): %v", msgNum, err)
		}
		hdr.Subfields = edit(hdr.Subfields)
		hdr.SubfieldLen = 0
		for _, sf := range hdr.Subfields {
			hdr.SubfieldLen += SubfieldHdrSize + sf.DatLen
		}
		offset, err := b.writeMessageHeader(hdr)
		if err != nil {
			t.Fatalf("writeMessageHeader(%d): %v", msgNum, err)
		}
		if err := b.writeIndexRecord(msgNum, &IndexRecord{ToCRC: CRC32String("someone"), HdrOffset: offset}); err != nil {
			t.Fatalf("writeIndexRecord(%d): %v", msgNum, err)
		}
	}
	rewrite(none, func(sfs []Subfield) []Subfield {
		var kept []Subfield
		for _, sf := range sfs {
			if sf.LoID != SfldReceiverName {
				kept = append(kept, sf)
			}
		}
		return kept
	})
	rewrite(twice, func(sfs []Subfield) []Subfield {
		return append(sfs, CreateSubfield(SfldReceiverName, "Duplicate"))
	})

	for _, msgNum := range []int{none, twice} {
		if err := b.SetReceiverName(msgNum, "Sysop"); err != nil {
			t.Fatalf("SetReceiverName(%d): %v", msgNum, err)
		}
		hdr, err := b.ReadMessageHeader(msgNum)
		if err != nil {
			t.Fatalf("ReadMessageHeader(%d) after: %v", msgNum, err)
		}
		got := hdr.GetAllSubfieldsByType(SfldReceiverName)
		if len(got) != 1 || string(got[0].Buffer) != "Sysop" {
			t.Errorf("message %d receiver subfields = %q, want exactly [Sysop]", msgNum, got)
		}
		// SubfieldLen must describe what was written or the next reader
		// would run into the following header.
		var want uint32
		for _, sf := range hdr.Subfields {
			want += SubfieldHdrSize + sf.DatLen
		}
		if hdr.SubfieldLen != want {
			t.Errorf("message %d SubfieldLen = %d, want %d", msgNum, hdr.SubfieldLen, want)
		}
		if sender := hdr.GetSubfieldByType(SfldSenderName); sender == nil || string(sender.Buffer) != "Sysop" {
			t.Errorf("message %d lost its sender subfield: %v", msgNum, sender)
		}
	}
	if got, err := b.CountMessagesToUser("sysop"); err != nil || got != 2 {
		t.Errorf("CountMessagesToUser(sysop) = %d, %v; want 2", got, err)
	}
}

func TestSetReceiverNameErrors(t *testing.T) {
	b, _ := openCovTestBase(t)
	writeCovMsg(t, b, "Sysop", "All", "Only", "body")

	for _, msgNum := range []int{0, 2} {
		if err := b.SetReceiverName(msgNum, "Nobody"); !errors.Is(err, ErrInvalidMessage) {
			t.Errorf("SetReceiverName(%d) = %v, want ErrInvalidMessage", msgNum, err)
		}
	}
	// A failed call must leave the message as it was.
	if msg, err := b.ReadMessage(1); err != nil || msg.To != "All" {
		t.Errorf("message after failed readdress: to=%q err=%v", msg.To, err)
	}

	// A message whose stored header is unreadable cannot be readdressed.
	idx, err := b.ReadIndexRecord(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.jhrFile.WriteAt([]byte("XXXX"), int64(idx.HdrOffset)); err != nil {
		t.Fatal(err)
	}
	if err := b.SetReceiverName(1, "Nobody"); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("SetReceiverName on corrupt header = %v, want ErrInvalidSignature", err)
	}

	b.Close()
	if err := b.SetReceiverName(1, "Nobody"); !errors.Is(err, ErrBaseNotOpen) {
		t.Errorf("SetReceiverName on closed base = %v, want ErrBaseNotOpen", err)
	}
}

func TestWriteFixedHeaderFieldsAtLeavesSubfieldsAlone(t *testing.T) {
	b, _ := openCovTestBase(t)
	writeCovMsg(t, b, "Sysop", "All", "Subject line", "body")

	idx, err := b.ReadIndexRecord(1)
	if err != nil {
		t.Fatal(err)
	}
	hdr, err := b.ReadMessageHeader(1)
	if err != nil {
		t.Fatal(err)
	}
	changed := *hdr
	changed.TimesRead = 7
	changed.Attribute |= MsgRead
	if err := b.writeFixedHeaderFieldsAt(int64(idx.HdrOffset), &changed); err != nil {
		t.Fatalf("writeFixedHeaderFieldsAt: %v", err)
	}

	msg, err := b.ReadMessage(1)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msg.Header.TimesRead != 7 || msg.Header.Attribute&MsgRead == 0 {
		t.Errorf("fixed fields not updated: timesRead=%d attribute=%08x", msg.Header.TimesRead, msg.Header.Attribute)
	}
	if msg.From != "Sysop" || msg.To != "All" || msg.Subject != "Subject line" || msg.Text != "body" {
		t.Errorf("subfields or text disturbed: %q %q %q %q", msg.From, msg.To, msg.Subject, msg.Text)
	}

	// With the header file gone the write is reported, not swallowed.
	b.jhrFile.Close()
	err = b.writeFixedHeaderFieldsAt(int64(idx.HdrOffset), &changed)
	if err == nil || !strings.Contains(err.Error(), "jam:") {
		t.Errorf("writeFixedHeaderFieldsAt on closed file = %v, want jam error", err)
	}
}

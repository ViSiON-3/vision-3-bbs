package jam

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPackEmptyBase(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "empty")

	b, err := Open(basePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	result, err := b.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if result.MessagesBefore != 0 || result.MessagesAfter != 0 || result.DeletedRemoved != 0 {
		t.Errorf("Pack empty: got before=%d after=%d deleted=%d",
			result.MessagesBefore, result.MessagesAfter, result.DeletedRemoved)
	}
}

func TestPackNoDeleted(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "nodeleted")

	b, err := Open(basePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	// Write 5 messages
	for i := 1; i <= 5; i++ {
		msg := NewMessage()
		msg.From = "Sender"
		msg.To = "All"
		msg.Subject = fmt.Sprintf("Message %d", i)
		msg.Text = fmt.Sprintf("Body of message %d", i)
		if _, err := b.WriteMessage(msg); err != nil {
			t.Fatalf("WriteMessage %d: %v", i, err)
		}
	}

	result, err := b.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if result.MessagesBefore != 5 || result.MessagesAfter != 5 || result.DeletedRemoved != 0 {
		t.Errorf("Pack no-deleted: got before=%d after=%d deleted=%d",
			result.MessagesBefore, result.MessagesAfter, result.DeletedRemoved)
	}

	// Verify messages are still readable with correct text
	for i := 1; i <= 5; i++ {
		msg, err := b.ReadMessage(i)
		if err != nil {
			t.Errorf("ReadMessage %d after pack: %v", i, err)
			continue
		}
		expected := fmt.Sprintf("Body of message %d", i)
		if msg.Text != expected {
			t.Errorf("Message %d text: got %q, want %q", i, msg.Text, expected)
		}
		if msg.Subject != fmt.Sprintf("Message %d", i) {
			t.Errorf("Message %d subject: got %q", i, msg.Subject)
		}
	}
}

func TestPackWithDeleted(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "withdeleted")

	b, err := Open(basePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	// Write 10 messages
	for i := 1; i <= 10; i++ {
		msg := NewMessage()
		msg.From = "Sender"
		msg.To = "All"
		msg.Subject = fmt.Sprintf("Message %d", i)
		msg.Text = fmt.Sprintf("Body of message %d with some extra text to take up space", i)
		if _, err := b.WriteMessage(msg); err != nil {
			t.Fatalf("WriteMessage %d: %v", i, err)
		}
	}

	// Delete messages 3, 5, 7
	for _, n := range []int{3, 5, 7} {
		if err := b.DeleteMessage(n); err != nil {
			t.Fatalf("DeleteMessage %d: %v", n, err)
		}
	}

	// Record sizes before pack
	sizeBefore := int64(0)
	for _, ext := range []string{".jhr", ".jdt", ".jdx"} {
		info, err := os.Stat(basePath + ext)
		if err != nil {
			t.Fatalf("Stat %s before pack: %v", basePath+ext, err)
		}
		sizeBefore += info.Size()
	}

	result, err := b.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if result.MessagesBefore != 10 {
		t.Errorf("MessagesBefore: got %d, want 10", result.MessagesBefore)
	}
	if result.MessagesAfter != 7 {
		t.Errorf("MessagesAfter: got %d, want 7", result.MessagesAfter)
	}
	if result.DeletedRemoved != 3 {
		t.Errorf("DeletedRemoved: got %d, want 3", result.DeletedRemoved)
	}

	// File sizes should have decreased
	sizeAfter := int64(0)
	for _, ext := range []string{".jhr", ".jdt", ".jdx"} {
		info, err := os.Stat(basePath + ext)
		if err != nil {
			t.Fatalf("Stat %s after pack: %v", basePath+ext, err)
		}
		sizeAfter += info.Size()
	}
	if sizeAfter >= sizeBefore {
		t.Errorf("Files did not shrink: before=%d after=%d", sizeBefore, sizeAfter)
	}

	// Verify remaining messages are readable
	count, _ := b.GetMessageCount()
	if count != 7 {
		t.Fatalf("GetMessageCount after pack: got %d, want 7", count)
	}

	// Original messages 1,2,4,6,8,9,10 should now be at positions 1-7
	expectedOriginals := []int{1, 2, 4, 6, 8, 9, 10}
	for i, origNum := range expectedOriginals {
		msg, err := b.ReadMessage(i + 1)
		if err != nil {
			t.Errorf("ReadMessage %d: %v", i+1, err)
			continue
		}
		expected := fmt.Sprintf("Body of message %d with some extra text to take up space", origNum)
		if msg.Text != expected {
			t.Errorf("Message %d (orig %d) text mismatch", i+1, origNum)
		}
	}
}

func TestPackPreservesLastread(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "lastread")

	b, err := Open(basePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	// Write a message and set lastread
	msg := NewMessage()
	msg.From = "Test"
	msg.To = "All"
	msg.Subject = "Test"
	msg.Text = "Test body"
	b.WriteMessage(msg)
	b.SetLastRead("testuser", 1, 1)

	result, err := b.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if result.MessagesAfter != 1 {
		t.Errorf("MessagesAfter: got %d, want 1", result.MessagesAfter)
	}

	// Nothing was removed, so the numbering is untouched and the pointer keeps
	// its value. What matters is that it still names the same message, which the
	// check below asserts -- a pack that dropped messages would have to move the
	// pointer to keep that true. See TestPackRemapsLastreadOntoNewNumbering.

	// Verify lastread still works
	lr, err := b.GetLastRead("testuser")
	if err != nil {
		t.Fatalf("GetLastRead after pack: %v", err)
	}
	if lr.LastReadMsg != 1 || lr.HighReadMsg != 1 {
		t.Errorf("LastRead: got %d/%d, want 1/1", lr.LastReadMsg, lr.HighReadMsg)
	}
}

func TestPackPreservesFixedHeader(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "fixedhdr")

	b, err := Open(basePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	fhBefore := b.GetFixedHeader()
	origCreated := fhBefore.DateCreated
	origBaseMsgNum := fhBefore.BaseMsgNum

	// Get a serial number to set the counter
	b.GetNextMsgSerial()
	serialBefore := binary.LittleEndian.Uint32(b.GetFixedHeader().Reserved[0:4])

	msg := NewMessage()
	msg.From = "Test"
	msg.To = "All"
	msg.Subject = "Test"
	msg.Text = "Body"
	b.WriteMessage(msg)

	if _, err := b.Pack(); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	fhAfter := b.GetFixedHeader()
	if fhAfter.DateCreated != origCreated {
		t.Errorf("DateCreated changed: %d -> %d", origCreated, fhAfter.DateCreated)
	}
	if fhAfter.BaseMsgNum != origBaseMsgNum {
		t.Errorf("BaseMsgNum changed: %d -> %d", origBaseMsgNum, fhAfter.BaseMsgNum)
	}
	serialAfter := binary.LittleEndian.Uint32(fhAfter.Reserved[0:4])
	if serialAfter != serialBefore {
		t.Errorf("Serial changed: %d -> %d", serialBefore, serialAfter)
	}
}

func TestPackRenameFailureMarksBaseClosed(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "renamefail")

	b, err := Open(basePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	msg := NewMessage()
	msg.From = "Sender"
	msg.To = "All"
	msg.Subject = "Test"
	msg.Text = "Body"
	if _, err := b.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	// Force the atomic rename to fail: replace the .jhr file with a
	// non-empty directory. The open handle still reads the unlinked file,
	// but os.Rename onto a non-empty directory fails, and the recovery
	// reopen of the original path fails too.
	//
	// The injection depends on unlinking a file that is still open, which
	// Windows refuses -- os.Remove there fails with a sharing violation while
	// the base holds the handle. The behaviour under test (a failed rename
	// leaves the base closed rather than open with nil handles) is
	// platform-independent; only this way of provoking it is not.
	if runtime.GOOS == "windows" {
		t.Skip("cannot unlink an open file on Windows, so the rename failure cannot be injected this way")
	}
	jhrPath := basePath + ".jhr"
	if err := os.Remove(jhrPath); err != nil {
		t.Fatalf("remove .jhr: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(jhrPath, "child"), 0755); err != nil {
		t.Fatalf("mkdir over .jhr: %v", err)
	}

	if _, err := b.Pack(); err == nil {
		t.Fatal("expected Pack to fail when rename target is a directory")
	}

	// The base could not be recovered (reopen failed), so it must not
	// claim to be open with nil file handles.
	if b.IsOpen() {
		t.Error("base claims open after failed rename recovery; want IsOpen() == false")
	}
}

// TestPackRemapsLastreadOntoNewNumbering covers the pointer bug behind
// "new private mail was never announced": packing renumbers surviving messages
// from 1, so a pointer left on the old numbering stops describing what the user
// has actually read. Once the base is smaller than the pointer, the login mail
// scan (which counts from lastread+1) reports nothing no matter what arrives.
func TestPackRemapsLastreadOntoNewNumbering(t *testing.T) {
	write := func(t *testing.T, b *Base, subject string) {
		t.Helper()
		m := NewMessage()
		m.From, m.To, m.Subject, m.Text = "someone", "sysop", subject, "body"
		if _, err := b.WriteMessage(m); err != nil {
			t.Fatalf("WriteMessage(%s): %v", subject, err)
		}
	}

	t.Run("pointer follows its message when earlier ones are packed away", func(t *testing.T) {
		b, err := Open(filepath.Join(t.TempDir(), "follow"))
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()

		for _, s := range []string{"one", "two", "three", "four"} {
			write(t, b, s)
		}
		// Read up to #3, then #1 and #2 are removed: "three" becomes #1.
		if err := b.SetLastRead("sysop", 3, 3); err != nil {
			t.Fatal(err)
		}
		for _, n := range []int{1, 2} {
			if err := b.DeleteMessage(n); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := b.Pack(); err != nil {
			t.Fatal(err)
		}

		lr, err := b.GetLastRead("sysop")
		if err != nil {
			t.Fatal(err)
		}
		if lr.LastReadMsg != 1 || lr.HighReadMsg != 1 {
			t.Fatalf("lastread = %d/%d, want 1/1 — the message read as #3 is now #1", lr.LastReadMsg, lr.HighReadMsg)
		}
		// "four" is the only message left unread, and must still read as unread.
		count, err := b.GetMessageCount()
		if err != nil {
			t.Fatal(err)
		}
		if unread := count - int(lr.LastReadMsg); unread != 1 {
			t.Errorf("unread count = %d, want 1 (\"four\")", unread)
		}
	})

	t.Run("mail arriving after an emptying pack is seen as new", func(t *testing.T) {
		b, err := Open(filepath.Join(t.TempDir(), "empty"))
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()

		// The board's case: every old private mail read, then removed.
		for _, s := range []string{"old one", "old two", "old three"} {
			write(t, b, s)
		}
		if err := b.SetLastRead("sysop", 3, 3); err != nil {
			t.Fatal(err)
		}
		for n := 1; n <= 3; n++ {
			if err := b.DeleteMessage(n); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := b.Pack(); err != nil {
			t.Fatal(err)
		}

		lr, err := b.GetLastRead("sysop")
		if err != nil {
			t.Fatal(err)
		}
		if lr.LastReadMsg != 0 || lr.HighReadMsg != 0 {
			t.Fatalf("lastread = %d/%d, want 0/0 — nothing survives to have been read", lr.LastReadMsg, lr.HighReadMsg)
		}

		// A new-user application arrives and becomes #1.
		write(t, b, "New user application")
		count, err := b.GetMessageCount()
		if err != nil {
			t.Fatal(err)
		}
		lr, err = b.GetLastRead("sysop")
		if err != nil {
			t.Fatal(err)
		}
		// This is the login mail scan's loop bound.
		newMail := 0
		for n := int(lr.LastReadMsg) + 1; n <= count; n++ {
			newMail++
		}
		if newMail != 1 {
			t.Errorf("login scan would report %d new messages, want 1 — the mail is invisible", newMail)
		}
	})

	t.Run("a pointer already past the end collapses to the new count", func(t *testing.T) {
		b, err := Open(filepath.Join(t.TempDir(), "stale"))
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()

		write(t, b, "only")
		// A pointer stranded by an earlier pack, naming messages that never
		// existed in this numbering.
		if err := b.SetLastRead("sysop", 9, 9); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Pack(); err != nil {
			t.Fatal(err)
		}
		lr, err := b.GetLastRead("sysop")
		if err != nil {
			t.Fatal(err)
		}
		if lr.LastReadMsg != 1 || lr.HighReadMsg != 1 {
			t.Errorf("lastread = %d/%d, want 1/1 — clamped to what the base actually holds", lr.LastReadMsg, lr.HighReadMsg)
		}
	})
}

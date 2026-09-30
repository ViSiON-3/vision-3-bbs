package jam

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// sampleHeader returns a header with every fixed field set to a distinct
// value and three subfields, one of them empty.
func sampleHeader() *MessageHeader {
	hdr := &MessageHeader{
		Revision:      1,
		ReservedWord:  2,
		TimesRead:     3,
		MSGIDcrc:      0x11111111,
		REPLYcrc:      0x22222222,
		ReplyTo:       4,
		Reply1st:      5,
		ReplyNext:     6,
		DateWritten:   7,
		DateReceived:  8,
		DateProcessed: 9,
		MessageNumber: 10,
		Attribute:     MsgLocal | MsgPrivate,
		Attribute2:    11,
		Offset:        12,
		TxtLen:        13,
		PasswordCRC:   0x33333333,
		Cost:          14,
		Subfields: []Subfield{
			CreateSubfield(SfldSenderName, "Sender"),
			CreateSubfield(SfldReceiverName, ""),
			CreateSubfield(SfldSubject, "A subject"),
		},
	}
	copy(hdr.Signature[:], Signature)
	for _, sf := range hdr.Subfields {
		hdr.SubfieldLen += SubfieldHdrSize + sf.DatLen
	}
	return hdr
}

// fieldsEqual compares two headers, treating nil and empty subfield buffers alike.
func fieldsEqual(a, b *MessageHeader) bool {
	x, y := *a, *b
	x.Subfields, y.Subfields = nil, nil
	if !reflect.DeepEqual(x, y) || len(a.Subfields) != len(b.Subfields) {
		return false
	}
	for i := range a.Subfields {
		sa, sb := a.Subfields[i], b.Subfields[i]
		if sa.LoID != sb.LoID || sa.HiID != sb.HiID || sa.DatLen != sb.DatLen || !bytes.Equal(sa.Buffer, sb.Buffer) {
			return false
		}
	}
	return true
}

func TestReadHeaderFromReaderEveryTruncationPoint(t *testing.T) {
	b, _ := openCovTestBase(t)
	hdr := sampleHeader()

	var buf bytes.Buffer
	if err := b.writeHeaderToWriter(&buf, hdr); err != nil {
		t.Fatalf("writeHeaderToWriter: %v", err)
	}
	full := buf.Bytes()
	// The JAM message header is 76 bytes before its subfields.
	if want := 76 + int(hdr.SubfieldLen); len(full) != want {
		t.Fatalf("encoded header is %d bytes, want %d", len(full), want)
	}

	got, err := b.readHeaderFromReader(bytes.NewReader(full))
	if err != nil {
		t.Fatalf("readHeaderFromReader(full): %v", err)
	}
	if !fieldsEqual(got, hdr) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, hdr)
	}

	// A header cut short anywhere, in a fixed field, a subfield header or a
	// subfield buffer, is an error and never a partially filled header.
	for n := 0; n < len(full); n++ {
		got, err := b.readHeaderFromReader(bytes.NewReader(full[:n]))
		if err == nil || got != nil {
			t.Errorf("header truncated to %d of %d bytes: got %v, err %v; want nil and an error", n, len(full), got, err)
		}
	}
}

func TestWriteHeaderToWriterEveryFailurePoint(t *testing.T) {
	b, _ := openCovTestBase(t)
	hdr := sampleHeader()

	// Count the writes a complete header takes; the empty subfield buffer
	// is not written at all.
	var total int
	counter := writerFunc(func(p []byte) (int, error) { total++; return len(p), nil })
	if err := b.writeHeaderToWriter(counter, hdr); err != nil {
		t.Fatalf("writeHeaderToWriter: %v", err)
	}
	if total < 20+3*len(hdr.Subfields) {
		t.Fatalf("header took %d writes, want at least one per field", total)
	}

	// Whichever write fails, the failure is reported to the caller.
	for ok := 0; ok < total; ok++ {
		err := b.writeHeaderToWriter(&errWriter{n: ok}, hdr)
		if err == nil || !strings.Contains(err.Error(), "jam: write") {
			t.Errorf("failure on write %d of %d: err = %v, want a jam write error", ok+1, total, err)
		}
	}
	if err := b.writeHeaderToWriter(&errWriter{n: total}, hdr); err != nil {
		t.Errorf("writer allowing all %d writes: err = %v", total, err)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestReadMessageHeaderTruncatedFile(t *testing.T) {
	b, _ := openCovTestBase(t)
	writeCovMsg(t, b, "Sysop", "All", "Kept", "kept body")
	last := writeCovMsg(t, b, "Sysop", "Some User", "Cut short", "cut body")

	idx, err := b.ReadIndexRecord(last)
	if err != nil {
		t.Fatal(err)
	}
	info, err := b.jhrFile.Stat()
	if err != nil {
		t.Fatal(err)
	}

	// Chop the .jhr back one byte at a time through the last header, as a
	// crash mid-append would leave it. Every cut must be reported.
	for size := info.Size() - 1; size >= int64(idx.HdrOffset); size-- {
		if err := b.jhrFile.Truncate(size); err != nil {
			t.Fatalf("truncate to %d: %v", size, err)
		}
		if hdr, err := b.ReadMessageHeader(last); err == nil {
			t.Fatalf(".jhr truncated to %d bytes (header starts at %d): got header %+v, want error", size, idx.HdrOffset, hdr)
		}
		if _, err := b.ReadMessage(last); err == nil {
			t.Fatalf(".jhr truncated to %d bytes: ReadMessage succeeded", size)
		}
	}

	// The intact message before it is unaffected.
	msg, err := b.ReadMessage(1)
	if err != nil || msg.Subject != "Kept" || msg.Text != "kept body" {
		t.Errorf("first message after truncation: %+v, err %v", msg, err)
	}
}

// packTempFiles lists leftover pack temp files for the base.
func packTempFiles(t *testing.T, basePath string) []string {
	t.Helper()
	matches, err := filepath.Glob(basePath + ".*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestPackSkipsUnusedIndexSlots(t *testing.T) {
	b, _ := openCovTestBase(t)
	for _, subject := range []string{"one", "two", "three"} {
		writeCovMsg(t, b, "Sysop", "All", subject, "body of "+subject)
	}
	// An index record of all ones marks a slot with no message.
	if err := b.writeIndexRecord(2, &IndexRecord{ToCRC: 0xFFFFFFFF, HdrOffset: 0xFFFFFFFF}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadMessage(2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ReadMessage of unused slot = %v, want ErrNotFound", err)
	}

	res, err := b.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if res.MessagesBefore != 3 || res.MessagesAfter != 2 || res.DeletedRemoved != 1 {
		t.Errorf("pack result = %+v, want 3 before, 2 after, 1 removed", res)
	}
	for n, want := range map[int]string{1: "one", 2: "three"} {
		msg, err := b.ReadMessage(n)
		if err != nil || msg.Subject != want || msg.Text != "body of "+want {
			t.Errorf("message %d after pack = %+v, err %v; want subject %q", n, msg, err, want)
		}
	}
}

func TestPackAbortsOnCorruptBase(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(t *testing.T, b *Base)
		wantErr string
	}{
		{
			name: "header signature overwritten",
			corrupt: func(t *testing.T, b *Base) {
				idx, err := b.ReadIndexRecord(2)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := b.jhrFile.WriteAt([]byte("XXXX"), int64(idx.HdrOffset)); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "failed to read header for msg 2",
		},
		{
			name: "text file truncated",
			corrupt: func(t *testing.T, b *Base) {
				if err := b.jdtFile.Truncate(3); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "failed to read text for msg 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, basePath := openCovTestBase(t)
			writeCovMsg(t, b, "Sysop", "All", "one", "first body")
			writeCovMsg(t, b, "Sysop", "All", "two", "second body")
			tt.corrupt(t, b)

			_, err := b.Pack()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Pack = %v, want error containing %q", err, tt.wantErr)
			}
			// An aborted pack leaves no temp files and does not replace
			// or close the original base.
			if left := packTempFiles(t, basePath); len(left) != 0 {
				t.Errorf("temp files left behind: %v", left)
			}
			if !b.IsOpen() {
				t.Error("base closed by an aborted pack")
			}
			if n, err := b.GetMessageCount(); err != nil || n != 2 {
				t.Errorf("message count after aborted pack = %d, %v; want 2", n, err)
			}
			if hdr, err := b.ReadMessageHeader(1); err != nil || hdr.MessageNumber != 1 {
				t.Errorf("first header after aborted pack = %+v, err %v", hdr, err)
			}
		})
	}
}

func TestPackTempFileCannotBeCreated(t *testing.T) {
	// A directory squatting on a temp file name makes that os.Create fail.
	for _, ext := range []string{".jhr.tmp", ".jdt.tmp", ".jdx.tmp"} {
		t.Run(ext, func(t *testing.T) {
			b, basePath := openCovTestBase(t)
			writeCovMsg(t, b, "Sysop", "All", "one", "first body")
			if err := os.Mkdir(basePath+ext, 0755); err != nil {
				t.Fatal(err)
			}

			_, err := b.Pack()
			want := "failed to create temp " + strings.TrimSuffix(ext, ".tmp")
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Pack = %v, want error containing %q", err, want)
			}
			// Temp files created before the failure are removed again;
			// only the obstructing directory remains.
			if left := packTempFiles(t, basePath); len(left) != 1 || left[0] != basePath+ext {
				t.Errorf("temp entries after failed pack = %v, want only %s", left, basePath+ext)
			}
			if msg, err := b.ReadMessage(1); err != nil || msg.Text != "first body" {
				t.Errorf("message after failed pack = %+v, err %v", msg, err)
			}
		})
	}
}

func TestOpenFailsWhenBaseFileIsObstructed(t *testing.T) {
	// A directory where a base file belongs cannot be opened or created as a
	// file; Open must report which file failed rather than return a base
	// with missing handles.
	t.Run("creating a new base", func(t *testing.T) {
		for _, ext := range []string{".jhr", ".jdt", ".jdx", ".jlr"} {
			basePath := filepath.Join(t.TempDir(), "new")
			if err := os.Mkdir(basePath+ext, 0755); err != nil {
				t.Fatal(err)
			}
			// A directory named .jhr makes the base look present but
			// incomplete, so it is removed and recreated successfully.
			if ext == ".jhr" {
				b, err := Open(basePath)
				if err != nil {
					t.Errorf("Open with a directory at .jhr = %v, want recreated base", err)
					continue
				}
				if n, err := b.GetMessageCount(); err != nil || n != 0 {
					t.Errorf("recreated base message count = %d, %v", n, err)
				}
				b.Close()
				continue
			}
			b, err := Open(basePath)
			if err == nil || !strings.Contains(err.Error(), "failed to create "+ext) {
				t.Errorf("Open with a directory at %s = %v, want create failure", ext, err)
			}
			if b != nil && b.IsOpen() {
				t.Errorf("base reports open after failing to create %s", ext)
				b.Close()
			}
		}
	})

	t.Run("opening an existing base", func(t *testing.T) {
		for _, ext := range []string{".jdt", ".jdx", ".jlr"} {
			basePath := filepath.Join(t.TempDir(), "existing")
			b, err := Open(basePath)
			if err != nil {
				t.Fatal(err)
			}
			b.Close()
			if err := os.Remove(basePath + ext); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(basePath+ext, 0755); err != nil {
				t.Fatal(err)
			}

			b, err = Open(basePath)
			if err == nil || !strings.Contains(err.Error(), "failed to open "+ext) {
				t.Errorf("Open with a directory at %s = %v, want open failure", ext, err)
			}
			if b != nil {
				t.Errorf("Open returned a base alongside the %s failure", ext)
				b.Close()
			}
		}
	})

	t.Run("parent is a file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "plainfile")
		if err := os.WriteFile(file, nil, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(filepath.Join(file, "base")); err == nil || !strings.Contains(err.Error(), "failed to create directory") {
			t.Errorf("Open under a regular file = %v, want directory failure", err)
		}
	})
}

func TestOpenRecreatesTruncatedBase(t *testing.T) {
	b, basePath := openCovTestBase(t)
	writeCovMsg(t, b, "Sysop", "All", "lost", "body")
	b.Close()

	// A .jhr shorter than the fixed header cannot be a JAM base.
	if err := os.Truncate(basePath+".jhr", HeaderSize-1); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(basePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()
	if n, err := reopened.GetMessageCount(); err != nil || n != 0 {
		t.Errorf("message count of recreated base = %d, %v; want 0", n, err)
	}
	if fh := reopened.GetFixedHeader(); fh == nil || string(fh.Signature[:]) != Signature || fh.BaseMsgNum != 1 {
		t.Errorf("recreated fixed header = %+v", fh)
	}
}

func TestClosedBaseAccessors(t *testing.T) {
	b, _ := openCovTestBase(t)
	b.Close()

	if _, err := b.GetAllLastReadRecords(); !errors.Is(err, ErrBaseNotOpen) {
		t.Errorf("GetAllLastReadRecords = %v, want ErrBaseNotOpen", err)
	}
	if _, err := b.ReadMessageHeader(1); !errors.Is(err, ErrBaseNotOpen) {
		t.Errorf("ReadMessageHeader = %v, want ErrBaseNotOpen", err)
	}
	if _, err := b.writeMessageHeader(sampleHeader()); !errors.Is(err, ErrBaseNotOpen) {
		t.Errorf("writeMessageHeader = %v, want ErrBaseNotOpen", err)
	}
	if err := b.writeIndexRecord(1, &IndexRecord{}); !errors.Is(err, ErrBaseNotOpen) {
		t.Errorf("writeIndexRecord = %v, want ErrBaseNotOpen", err)
	}
	if _, err := b.RefreshFixedHeader(); !errors.Is(err, ErrBaseNotOpen) {
		t.Errorf("RefreshFixedHeader = %v, want ErrBaseNotOpen", err)
	}
	if _, err := b.GetModCounter(); !errors.Is(err, ErrBaseNotOpen) {
		t.Errorf("GetModCounter = %v, want ErrBaseNotOpen", err)
	}
}

package qwk

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// readFixture loads a committed packet sample from testdata.
func readFixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	p := filepath.Join(append([]string{"testdata"}, parts...)...)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", p, err)
	}
	return data
}

func TestFixture_VISION3_QWK_Structure(t *testing.T) {
	data := readFixture(t, "vision3", "VISION3.QWK")

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("fixture is not a valid zip: %v", err)
	}

	names := make(map[string]bool)
	for _, f := range zr.File {
		names[strings.ToUpper(f.Name)] = true
	}
	for _, want := range []string{"CONTROL.DAT", "DOOR.ID", "MESSAGES.DAT", "001.NDX", "PERSONAL.NDX"} {
		if !names[want] {
			t.Errorf("VISION3.QWK fixture missing %s", want)
		}
	}
}

func TestFixture_VISION3_REP_Reads(t *testing.T) {
	data := readFixture(t, "vision3", "VISION3.REP")

	msgs, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREP on fixture failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("want 1 message in fixture, got %d", len(msgs))
	}
	if msgs[0].To != "SysOp" {
		t.Errorf("fixture reply To: want 'SysOp', got %q", msgs[0].To)
	}
	if !strings.Contains(msgs[0].Subject, "Welcome") {
		t.Errorf("fixture reply Subject: want to contain 'Welcome', got %q", msgs[0].Subject)
	}
}

// TestFixture_TESTBBS_QWK_ExtendedHeaders validates a real packet exported by a
// live ViSiON/3 test BBS (not the synthetic writer). It asserts structural
// invariants that must hold for ANY well-formed ViSiON/3 export — one HEADERS.DAT
// section per message keyed by the message's exact MESSAGES.DAT byte offset
// (Phase 5), a Message-ID synthesized from the packet's own BBS ID, the full
// subject carried in HEADERS.DAT, and every reply reference pointing at a real
// message in the same packet (Phase 4). Being count- and content-agnostic keeps
// it robust even though this fixture path doubles as a live export target.
func TestFixture_TESTBBS_QWK_ExtendedHeaders(t *testing.T) {
	data := readFixture(t, "vision3", "TESTBBS.QWK")

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("fixture is not a valid zip: %v", err)
	}

	var headersRaw, messagesRaw, controlRaw []byte
	for _, f := range zr.File {
		switch strings.ToUpper(f.Name) {
		case "HEADERS.DAT":
			headersRaw = readZipEntry(t, f)
		case "MESSAGES.DAT":
			messagesRaw = readZipEntry(t, f)
		case "CONTROL.DAT":
			controlRaw = readZipEntry(t, f)
		}
	}
	if headersRaw == nil {
		t.Fatal("live packet is missing HEADERS.DAT")
	}
	if messagesRaw == nil {
		t.Fatal("live packet is missing MESSAGES.DAT")
	}

	// The BBS ID is CONTROL.DAT line 5 ("serial,BBSID"); Message-IDs use it
	// lower-cased. Deriving it from the packet keeps the test board-name agnostic.
	bbsID := controlBBSID(controlRaw)
	if bbsID == "" {
		t.Fatal("could not read BBS ID from CONTROL.DAT line 5")
	}

	// Parse the message headers directly from MESSAGES.DAT (a .QWK stores messages
	// there, not in a .MSG, so ReadREPPacket does not apply).
	type msgHdr struct {
		offset, number, conf, ref int
		subject                   string
	}
	var msgs []msgHdr
	nums := map[int]bool{}
	for pos := BlockSize; pos+BlockSize <= len(messagesRaw); {
		h := messagesRaw[pos : pos+BlockSize]
		blocks, err := strconv.Atoi(strings.TrimSpace(string(h[116:122])))
		if err != nil || blocks < 1 {
			break
		}
		num, _ := strconv.Atoi(strings.TrimSpace(string(h[1:8])))
		ref, _ := strconv.Atoi(strings.TrimSpace(string(h[108:116])))
		msgs = append(msgs, msgHdr{
			offset:  pos,
			number:  num,
			conf:    int(h[123]) | int(h[124])<<8,
			ref:     ref,
			subject: strings.TrimSpace(string(h[71:96])),
		})
		nums[num] = true
		pos += blocks * BlockSize
	}
	if len(msgs) == 0 {
		t.Fatal("live packet contains no messages")
	}

	// Phase 5: exactly one HEADERS.DAT section per message.
	headers := parseHeadersDAT(headersRaw)
	if len(headers) != len(msgs) {
		t.Errorf("HEADERS.DAT sections = %d, want %d (one per message)", len(headers), len(msgs))
	}

	for _, m := range msgs {
		// Phase 4: any reply reference must point at a message present in the packet.
		if m.ref > 0 && !nums[m.ref] {
			t.Errorf("message #%d reference = %d, but no such message in packet", m.number, m.ref)
		}

		// Phase 5: section keyed by the message's exact byte offset (offset agreement).
		h, ok := headers[m.offset]
		if !ok {
			t.Errorf("HEADERS.DAT missing section for message #%d at offset %d (hex %x)", m.number, m.offset, m.offset)
			continue
		}
		// The base header truncates the subject to 25 chars; HEADERS.DAT carries the
		// full value, so the (trimmed) base subject must be a prefix of it.
		if !strings.HasPrefix(h.Subject, m.subject) {
			t.Errorf("message #%d subject: HEADERS.DAT %q does not extend base header %q", m.number, h.Subject, m.subject)
		}
		wantID := "<" + strconv.Itoa(m.number) + "." + strconv.Itoa(m.conf) + "@" + strings.ToLower(bbsID) + ">"
		if h.MessageID != wantID {
			t.Errorf("message #%d Message-ID: got %q, want %q", m.number, h.MessageID, wantID)
		}
	}
}

// controlBBSID extracts the BBS ID from CONTROL.DAT line 5, which has the form
// "serial,BBSID" (e.g. "00000,TESTBBS"). Returns "" if it cannot be found.
func controlBBSID(control []byte) string {
	lines := strings.Split(strings.ReplaceAll(string(control), "\r\n", "\n"), "\n")
	if len(lines) < 5 {
		return ""
	}
	_, id, ok := strings.Cut(lines[4], ",")
	if !ok {
		return ""
	}
	return strings.TrimSpace(id)
}

// readZipEntry reads a single zip entry fully.
func readZipEntry(t *testing.T, f *zip.File) []byte {
	t.Helper()
	rc, err := f.Open()
	if err != nil {
		t.Fatalf("open %s: %v", f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %s: %v", f.Name, err)
	}
	return data
}

func TestFixture_Malformed_REP_FailsGracefully(t *testing.T) {
	data := readFixture(t, "malformed", "TRUNCATED.REP")

	_, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err == nil {
		t.Fatal("expected ReadREP to reject the truncated fixture")
	}
}

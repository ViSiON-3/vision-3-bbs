package qwk

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestSplitBodyKludges(t *testing.T) {
	body := "@VIA: VERT/OTHER\n@msgid: <1.2@other>\n@REPLYTO: Some One@VERT\n@REPLY: <9.9@vert>\n@TZ: 41e0\nTo: Somebody With A Long Name\nHello there\n@VIA: not a kludge now\n"
	k, rest := splitBodyKludges(body)
	if k.via != "VERT/OTHER" || k.msgID != "<1.2@other>" || k.replyID != "<9.9@vert>" || k.replyTo != "Some One@VERT" || k.tz != "41e0" || k.to != "Somebody With A Long Name" {
		t.Fatalf("kludges = %+v", k)
	}
	if rest != "Hello there\n@VIA: not a kludge now\n" {
		t.Fatalf("rest = %q", rest)
	}
	if k2, rest2 := splitBodyKludges("plain\n"); k2 != (bodyKludges{}) || rest2 != "plain\n" {
		t.Fatalf("plain body altered: %+v %q", k2, rest2)
	}
}

func TestParseSMBTimezone(t *testing.T) {
	cases := map[string]int{
		"0000":  0,
		"41e0":  -480 * 60, // US Pacific standard
		"c1e0":  -420 * 60, // US Pacific daylight
		"21e0":  -480 * 60, // western, as smbTimezone writes
		"103c":  60 * 60,   // eastern +1
		"ffe2":  -30 * 60,  // plain signed minutes
		"0x1e0": 480 * 60,  // signed within range
	}
	for in, want := range cases {
		got, ok := ParseSMBTimezone(in)
		if !ok || got != want {
			t.Errorf("ParseSMBTimezone(%q) = %d,%v want %d", in, got, ok, want)
		}
	}
	if _, ok := ParseSMBTimezone("zz"); ok {
		t.Error("garbage accepted")
	}
	// Round trip through the writer's encoder.
	for _, off := range []int{-8 * 3600, 3600, 0} {
		got, ok := ParseSMBTimezone(smbTimezone(off))
		if !ok || got != off {
			t.Errorf("round trip %d -> %d", off, got)
		}
	}
}

func TestParseControlDAT(t *testing.T) {
	data := []byte("Vertrauen\r\nAnaheim, CA\r\n714-529-9525\r\nDigital Man\r\n12345,VERT\r\n01-02-2026,10:00\r\nNODE1\r\n\r\n0\r\n7\r\n2\r\n2001\r\nDOVE-Net General\r\n2002\r\nDOVE-Net Ads\r\n2006\r\nProgramming\r\nHELLO\r\nNEWS\r\nGOODBYE\r\n")
	id, name, confs := parseControlDAT(data)
	if id != "VERT" || name != "Vertrauen" {
		t.Fatalf("id=%q name=%q", id, name)
	}
	if len(confs) != 3 || confs[0].Number != 2001 || confs[2].Name != "Programming" {
		t.Fatalf("confs = %+v", confs)
	}
}

func TestWriteNetREP_RoundTrip(t *testing.T) {
	pst := time.FixedZone("PST", -8*3600)
	in := []NetMessage{
		{
			Conference: 2001, From: "Robbie", To: "All", Subject: "A subject longer than twenty-five chars",
			DateTime:  time.Date(2026, 9, 24, 14, 30, 0, 0, pst),
			Body:      "First line\nSecond line",
			MessageID: "<12.dove_gen@vision3>", ReplyID: "<7.x@vert>",
		},
		{
			Conference: 2006, From: "Someone", To: "Robbie", Subject: "Re: hi",
			DateTime: time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC),
			Body:     "Reply text\n---\n \xfe Other BBS \xfe already tagged\n",
			Private:  true, MessageID: "<13.dove_prog@vision3>",
		},
	}
	var buf bytes.Buffer
	if err := WriteNetREP(&buf, "vert", in, NetREPOptions{Tagline: "ViSiON/3 Test Node", NodeID: "VISION3"}); err != nil {
		t.Fatal(err)
	}

	p, err := ReadREPPacket(bytes.NewReader(buf.Bytes()), int64(buf.Len()), "VERT")
	if err != nil {
		t.Fatal(err)
	}
	if p.BBSID != "VERT" {
		t.Errorf("first block id = %q", p.BBSID)
	}
	headers := map[int]ExtHeader{}
	// Re-read with the network parser, headers included.
	id, msgs, perr := ParseMSGPayload(p.Payload, headersFromREP(t, buf.Bytes()))
	if perr != nil || id != "VERT" || len(msgs) != 2 {
		t.Fatalf("ParseMSGPayload: id=%q n=%d err=%v", id, len(msgs), perr)
	}
	_ = headers
	m := msgs[0]
	if m.Conference != 2001 || m.Number != 2001 {
		t.Errorf("conference fields: conf=%d number=%d", m.Conference, m.Number)
	}
	if m.Subject != in[0].Subject || m.From != "Robbie" || m.To != "All" {
		t.Errorf("names: %q %q %q", m.Subject, m.From, m.To)
	}
	if m.MessageID != in[0].MessageID || m.ReplyID != in[0].ReplyID {
		t.Errorf("ids: %q %q", m.MessageID, m.ReplyID)
	}
	if !m.DateTime.Equal(in[0].DateTime) {
		t.Errorf("date = %v want %v", m.DateTime, in[0].DateTime)
	}
	if !strings.HasPrefix(m.Body, "First line\nSecond line") || !strings.Contains(m.Body, "\xfe ViSiON/3 \xfe ViSiON/3 Test Node") {
		t.Errorf("body = %q", m.Body)
	}
	if strings.Contains(m.Body, "@MSGID") {
		t.Errorf("kludges leaked into body: %q", m.Body)
	}
	if strings.Count(msgs[1].Body, "---") != 1 || !msgs[1].Private {
		t.Errorf("second message: tearline count or private wrong: %q private=%v", msgs[1].Body, msgs[1].Private)
	}
}

// headersFromREP pulls HEADERS.DAT out of a REP archive for the test.
func headersFromREP(t *testing.T, archive []byte) map[int]ExtHeader {
	t.Helper()
	p, err := ReadArchiveHeaders(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWriteNetREP_NoHeadersStillCarriesKludges(t *testing.T) {
	var buf bytes.Buffer
	msg := NetMessage{Conference: 5, From: "A", To: "B", Subject: "S", DateTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Body: "x", MessageID: "<1@a>"}
	if err := WriteNetREP(&buf, "HUB", []NetMessage{msg}, NetREPOptions{NoHeaders: true}); err != nil {
		t.Fatal(err)
	}
	if headersFromREP(t, buf.Bytes()) != nil {
		t.Error("HEADERS.DAT written despite NoHeaders")
	}
	p, err := ReadREPPacket(bytes.NewReader(buf.Bytes()), int64(buf.Len()), "HUB")
	if err != nil {
		t.Fatal(err)
	}
	_, msgs, _ := ParseMSGPayload(p.Payload, nil)
	if len(msgs) != 1 || msgs[0].MessageID != "<1@a>" || msgs[0].Body != "x" {
		t.Fatalf("msgs = %+v", msgs)
	}
}

func TestReadPacket_SynchronetStyle(t *testing.T) {
	// Build a hub-style packet by hand: CONTROL.DAT, MESSAGES.DAT with body
	// kludges and no HEADERS.DAT, the way an older hub would send it.
	control := "Hub\r\nCity\r\n000\r\nSysop\r\n1,HUB\r\n01-01-2026,00:00\r\nNODE\r\n\r\n0\r\n1\r\n0\r\n2001\r\nGeneral\r\n"
	body := "@VIA: HUB/FAR\xe3@MSGID: <77.gen@far>\xe3@TZ: 412c\xe3Hello from far away\xe3"
	hdr := bytes.Repeat([]byte{' '}, BlockSize)
	hdr[0] = ' '
	copy(hdr[1:8], "     77")
	copy(hdr[8:21], "03-15-2609:45")
	copy(hdr[21:46], "All")
	copy(hdr[46:71], "Far User")
	copy(hdr[71:96], "Greetings")
	copy(hdr[108:116], "       0")
	blocks := 1 + (len(body)+BlockSize-1)/BlockSize
	copy(hdr[116:122], []byte(padLeft(blocks, 6)))
	hdr[122] = 0xE1
	hdr[123] = byte(2001 & 0xff)
	hdr[124] = byte(2001 >> 8)
	var msgs bytes.Buffer
	msgs.Write(bytes.Repeat([]byte{' '}, BlockSize))
	msgs.Write(hdr)
	padded := bytes.Repeat([]byte{' '}, (blocks-1)*BlockSize)
	copy(padded, body)
	msgs.Write(padded)

	archive := buildZip(t, map[string][]byte{"control.dat": []byte(control), "messages.dat": msgs.Bytes()})
	p, err := ReadPacket(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	if p.BBSID != "HUB" || len(p.Conferences) != 1 || p.Conferences[0].Number != 2001 {
		t.Fatalf("control: %+v", p)
	}
	if len(p.Messages) != 1 {
		t.Fatalf("messages = %d (%v)", len(p.Messages), p.ParseError)
	}
	m := p.Messages[0]
	if m.Conference != 2001 || m.Number != 77 || m.From != "Far User" || m.Via != "HUB/FAR" || m.MessageID != "<77.gen@far>" {
		t.Errorf("message = %+v", m)
	}
	if m.Body != "Hello from far away" {
		t.Errorf("body = %q", m.Body)
	}
	want := time.Date(2026, 3, 15, 9, 45, 0, 0, time.FixedZone("", -300*60))
	if !m.DateTime.Equal(want) {
		t.Errorf("date = %v want %v", m.DateTime, want)
	}
}

func TestReadPacket_EmptyPacket(t *testing.T) {
	archive := buildZip(t, map[string][]byte{"CONTROL.DAT": []byte("Hub\r\n\r\n\r\n\r\n1,HUB\r\n")})
	p, err := ReadPacket(bytes.NewReader(archive), int64(len(archive)))
	if err != nil || p.BBSID != "HUB" || len(p.Messages) != 0 {
		t.Fatalf("empty packet: %+v err=%v", p, err)
	}
}

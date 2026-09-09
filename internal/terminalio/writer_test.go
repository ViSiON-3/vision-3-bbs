package terminalio

import (
	"bytes"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

func TestWriteProcessedBytes_UTF8Mode_MapsCP437InvalidSpan(t *testing.T) {
	// Use CP437 bytes separated by ASCII space to prevent UTF-8 sequence formation
	// 0xB3 = │ (vertical line), 0xBA = ║ (double vertical line)
	input := []byte{0xB3, 0x20, 0xBA} // │ ║ in CP437

	var out bytes.Buffer
	err := WriteProcessedBytes(&out, input, ansi.OutputModeUTF8)
	if err != nil {
		t.Fatalf("WriteProcessedBytes returned error: %v", err)
	}

	want := "│ ║"
	if out.String() != want {
		t.Fatalf("unexpected output: got %q want %q", out.String(), want)
	}
}

func TestWriteProcessedBytes_UTF8Mode_PreservesValidUTF8Span(t *testing.T) {
	input := []byte("Hello π")

	var out bytes.Buffer
	err := WriteProcessedBytes(&out, input, ansi.OutputModeUTF8)
	if err != nil {
		t.Fatalf("WriteProcessedBytes returned error: %v", err)
	}

	if !bytes.Equal(out.Bytes(), input) {
		t.Fatalf("valid UTF-8 should pass through unchanged: got %q want %q", out.String(), string(input))
	}
}

func TestWriteProcessedBytes_UTF8Mode_PreservesANSIAndMapsCP437(t *testing.T) {
	// Use CP437 bytes separated by spaces to prevent UTF-8 sequence formation
	input := []byte("\x1b[31m\xB3\x20\xBA\x1b[0m")

	var out bytes.Buffer
	err := WriteProcessedBytes(&out, input, ansi.OutputModeUTF8)
	if err != nil {
		t.Fatalf("WriteProcessedBytes returned error: %v", err)
	}

	want := "\x1b[31m│ ║\x1b[0m"
	if out.String() != want {
		t.Fatalf("unexpected output with ANSI: got %q want %q", out.String(), want)
	}
}

// TestWriteProcessedBytes_UTF8Mode_CP437PairsThatLookLikeUTF8 covers the pairs
// that made CP437 art unreadable. Deciding the encoding one rune at a time
// looks like it separates mixed content, but adjacent CP437 bytes routinely
// form a structurally valid UTF-8 sequence — the decoder then consumes both
// and emits one unrelated character. Line art is dense with such pairs, so the
// content most likely to be CP437 was the content most likely to be misread
// (#280).
func TestWriteProcessedBytes_UTF8Mode_CP437PairsThatLookLikeUTF8(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{
			// DC B3 is a well-formed two-byte UTF-8 sequence for U+0733, a
			// Syriac combining mark, and used to be emitted as one. In a span
			// that is CP437 overall it is two characters.
			name: "lower half block then vertical line",
			in:   []byte{0xDA, 0xC4, 0xDC, 0xB3},
			want: "┌─▄│",
		},
		{
			// C4 B3 decodes as U+0133 on its own.
			name: "horizontal line then vertical line",
			in:   []byte{0xDA, 0xC4, 0xB3, 0xBF},
			want: "┌─│┐",
		},
		{
			// A real line lifted from an fsxNet message that rendered as
			// mojibake on a live board.
			name: "art line from a real message",
			in: []byte{
				0x3a, 0x20, 0x20, 0x20, 0x3a, 0x20,
				0xda, 0xc4, 0xdc, 0xb3, 0x20, 0xda, 0xc4, 0xdc,
				0xda, 0xc4, 0xdc, 0xde, 0xc4, 0xdc,
			},
			want: ":   : ┌─▄│ ┌─▄┌─▄▐─▄",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteProcessedBytes(&buf, tt.in, ansi.OutputModeUTF8); err != nil {
				t.Fatalf("WriteProcessedBytes: %v", err)
			}
			if got := buf.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestWriteProcessedBytes_UTF8Mode_MixedSpanPrefersCP437 pins the trade-off
// made when a span cannot be both. A span carrying any invalid sequence is
// read as CP437 throughout, because a body that is truly UTF-8 is valid
// throughout and takes the passthrough path above.
func TestWriteProcessedBytes_UTF8Mode_MixedSpanPrefersCP437(t *testing.T) {
	// "é" as UTF-8 (C3 A9) followed by a byte that cannot continue any
	// sequence, so the span as a whole is invalid.
	in := []byte{0xC3, 0xA9, 0xDB}
	var buf bytes.Buffer
	if err := WriteProcessedBytes(&buf, in, ansi.OutputModeUTF8); err != nil {
		t.Fatalf("WriteProcessedBytes: %v", err)
	}
	want := "├⌐█" // C3, A9 and DB each read as CP437
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestWriteProcessedBytes_UTF8Mode_ShortAmbiguousSpanStaysUTF8 records what
// this heuristic cannot do. A span short enough to be valid under both
// encodings is genuinely ambiguous — DC B3 is both "▄│" in CP437 and U+0733 in
// UTF-8 — and is read as UTF-8. Only the message's own CHRS kludge can settle
// it, so plumbing that through to the writer is the real fix; until then this
// is the residual case, pinned here so a future change to it is deliberate.
func TestWriteProcessedBytes_UTF8Mode_ShortAmbiguousSpanStaysUTF8(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteProcessedBytes(&buf, []byte{0xDC, 0xB3}, ansi.OutputModeUTF8); err != nil {
		t.Fatalf("WriteProcessedBytes: %v", err)
	}
	if got, want := buf.String(), "\u0733"; got != want {
		t.Errorf("got %q, want %q — a two-byte span valid under both encodings is read as UTF-8", got, want)
	}
}

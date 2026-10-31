package editor

import (
	"strings"
	"testing"
)

// A quoted author or subject only gets to colour the banner, which is expanded
// into the message buffer; the banner's own codes still act.
func TestQuoteBannerEscapesAuthorScreenCodes(t *testing.T) {
	ch := &CommandHandler{quoteData: &QuoteData{From: "a|CLb", Title: "c|PPd"}, quoteTopStr: "|04^N ^T"}
	top, _ := ch.quoteBanners()
	if strings.Contains(top, "\x1b[2J") || strings.Contains(top, "\x1b[u") {
		t.Errorf("the banner ran a screen code: %q", top)
	}
	if !strings.Contains(top, "a|CLb") || !strings.HasPrefix(top, "\x1b[") {
		t.Errorf("banner = %q", top)
	}
}

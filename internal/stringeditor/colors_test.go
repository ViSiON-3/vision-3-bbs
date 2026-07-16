package stringeditor

import "testing"

func TestParseColorCodes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []styledSpan
	}{
		{"plain text single span", "hello", []styledSpan{{text: "hello", fg: dosColors[9]}}},
		{"pipe fg splits spans", "a|04b", []styledSpan{
			{text: "a", fg: dosColors[9]},
			{text: "b", fg: dosColors[4]},
		}},
		{"pipe fg 15", "|15x", []styledSpan{{text: "x", fg: dosColors[15]}}},
		{"background code", "|B1x", []styledSpan{{text: "x", fg: dosColors[9], bg: dosBgColors[1]}}},
		{"CR becomes space", "a|CRb", []styledSpan{{text: "a b", fg: dosColors[9]}}},
		{"CL is stripped", "a|CLb", []styledSpan{{text: "ab", fg: dosColors[9]}}},
		{"dollar lowercase", "$rx", []styledSpan{{text: "x", fg: dosColors[4]}}},
		{"dollar uppercase", "$Wx", []styledSpan{{text: "x", fg: dosColors[15]}}},
		{"unknown dollar literal", "$zx", []styledSpan{{text: "$zx", fg: dosColors[9]}}},
		{"unknown pipe literal", "|ZZx", []styledSpan{{text: "|ZZx", fg: dosColors[9]}}},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseColorCodes(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("spans = %+v, want %+v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("span[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDollarColorIndex(t *testing.T) {
	tests := []struct {
		ch   byte
		want int
	}{
		{'a', 0}, {'b', 1}, {'g', 2}, {'c', 3}, {'r', 4}, {'p', 5}, {'y', 6}, {'w', 7},
		{'A', 8}, {'B', 9}, {'G', 10}, {'C', 11}, {'R', 12}, {'P', 13}, {'Y', 14}, {'W', 15},
		{'z', -1}, {'0', -1}, {' ', -1},
	}
	for _, tt := range tests {
		if got := dollarColorIndex(tt.ch); got != tt.want {
			t.Errorf("dollarColorIndex(%q) = %d, want %d", tt.ch, got, tt.want)
		}
	}
}

func TestPlainTextLength(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"plain", "hello", 5},
		{"pipe codes stripped", "|04red|15white", 8},
		{"dollar codes stripped", "$rab$Wcd", 4},
		{"CR counts as space", "a|CRb", 3},
		{"empty", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlainTextLength(tt.in); got != tt.want {
				t.Errorf("PlainTextLength(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestRenderColorString_TruncatesWithOverflowMarker(t *testing.T) {
	// 10 visible chars, maxWidth 5: output visible content is 4 chars + "»".
	out := RenderColorString("0123456789", 5)
	if visualLen(out) != 5 {
		t.Errorf("visible len = %d, want 5 (4 chars + overflow marker)", visualLen(out))
	}
	// Fits: no marker.
	out = RenderColorString("abc", 10)
	if visualLen(out) != 3 {
		t.Errorf("visible len = %d, want 3", visualLen(out))
	}
}

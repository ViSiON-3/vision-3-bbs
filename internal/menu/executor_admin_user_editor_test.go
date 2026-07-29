package menu

import "testing"

func TestVisualPipeWidth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"hello", 5},
		{"|09hello", 5},
		{"|09he|15llo", 5},
		{"", 0},
	}
	for _, c := range cases {
		if got := visualPipeWidth(c.in); got != c.want {
			t.Errorf("visualPipeWidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestComputeUserEditorLayout(t *testing.T) {
	l := computeUserEditorLayout(24)
	if l.pageSize <= 0 {
		t.Fatalf("pageSize = %d, want > 0", l.pageSize)
	}
	if computeUserEditorLayout(50).pageSize <= l.pageSize {
		t.Fatal("taller terminal must not shrink pageSize")
	}

	// Exact geometry for a 24-row terminal: pageSize = 24-14 = 10 (within the
	// [3,12] clamp), and every row is derived from listStartRow+pageSize.
	want := userEditorLayout{
		pageSize:       10,
		titleRow:       1,
		sepTopRow:      2,
		headerRow:      3,
		listStartRow:   4,
		sepMidRow:      14,
		detailStartRow: 15,
		statusRow:      23,
		actionRow:      24,
	}
	if l != want {
		t.Errorf("computeUserEditorLayout(24) = %+v, want %+v", l, want)
	}

	// A very short terminal clamps pageSize to the minimum of 3.
	if got := computeUserEditorLayout(10).pageSize; got != 3 {
		t.Errorf("computeUserEditorLayout(10).pageSize = %d, want 3 (clamped minimum)", got)
	}

	// A very tall terminal clamps pageSize to the maximum of 12.
	if got := computeUserEditorLayout(100).pageSize; got != 12 {
		t.Errorf("computeUserEditorLayout(100).pageSize = %d, want 12 (clamped maximum)", got)
	}
}

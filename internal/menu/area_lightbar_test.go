package menu

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// newTestPicker builds a picker over n string items with a simple item-line
// builder that records invocations, using a bytes.Buffer as the terminal.
func newTestPicker(n, visibleRows int) (*areaLightbarPicker[string], *bytes.Buffer, *[]string) {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf("item-%d", i)
	}
	var out bytes.Buffer
	calls := &[]string{}
	p := &areaLightbarPicker[string]{
		terminal:   &out,
		outputMode: ansi.OutputModeCP437,
		items:      items,
		buildItemLine: func(item string, displayIdx int) string {
			*calls = append(*calls, fmt.Sprintf("%s:%d", item, displayIdx))
			return item
		},
		termWidth:        80,
		termHeight:       24,
		itemAreaStartRow: 5,
		visibleRows:      visibleRows,
	}
	return p, &out, calls
}

func TestClampSelectionBounds(t *testing.T) {
	p, _, _ := newTestPicker(10, 5)

	p.selectedIndex = -3
	p.clampSelection()
	if p.selectedIndex != 0 {
		t.Errorf("negative selection: got %d, want 0", p.selectedIndex)
	}

	p.selectedIndex = 42
	p.clampSelection()
	if p.selectedIndex != 9 {
		t.Errorf("overshoot selection: got %d, want 9", p.selectedIndex)
	}
	// Selection past the window must scroll topIndex down.
	if p.topIndex != 5 {
		t.Errorf("topIndex after scroll down: got %d, want 5", p.topIndex)
	}

	// Selection above the window must scroll topIndex up.
	p.selectedIndex = 2
	p.clampSelection()
	if p.topIndex != 2 {
		t.Errorf("topIndex after scroll up: got %d, want 2", p.topIndex)
	}
}

func TestClampSelectionEmptyList(t *testing.T) {
	p, _, _ := newTestPicker(0, 5)
	p.selectedIndex, p.topIndex = 7, 3
	p.clampSelection()
	if p.selectedIndex != 0 || p.topIndex != 0 {
		t.Errorf("empty list: got (%d,%d), want (0,0)", p.selectedIndex, p.topIndex)
	}
}

func TestMoveSelection(t *testing.T) {
	p, _, _ := newTestPicker(20, 5)

	p.moveSelection(editor.KeyArrowDown)
	if p.selectedIndex != 1 {
		t.Errorf("arrow down: got %d, want 1", p.selectedIndex)
	}
	p.moveSelection(editor.KeyArrowUp)
	if p.selectedIndex != 0 {
		t.Errorf("arrow up: got %d, want 0", p.selectedIndex)
	}

	p.moveSelection(editor.KeyPageDown)
	if p.selectedIndex != 5 || p.topIndex != 5 {
		t.Errorf("page down: got (%d,%d), want (5,5)", p.selectedIndex, p.topIndex)
	}
	p.moveSelection(editor.KeyPageUp)
	if p.selectedIndex != 0 || p.topIndex != 0 {
		t.Errorf("page up: got (%d,%d), want (0,0)", p.selectedIndex, p.topIndex)
	}
	// Page up at the top must not push topIndex negative.
	p.moveSelection(editor.KeyPageUp)
	if p.topIndex != 0 {
		t.Errorf("page up at top: topIndex got %d, want 0", p.topIndex)
	}

	p.moveSelection(editor.KeyEnd)
	if p.selectedIndex != 19 {
		t.Errorf("end: got %d, want 19", p.selectedIndex)
	}
	p.moveSelection(editor.KeyHome)
	if p.selectedIndex != 0 {
		t.Errorf("home: got %d, want 0", p.selectedIndex)
	}

	// Digit shortcuts: '3' selects index 2, '0' selects index 9.
	p.moveSelection('3')
	if p.selectedIndex != 2 {
		t.Errorf("digit 3: got %d, want 2", p.selectedIndex)
	}
	p.moveSelection('0')
	if p.selectedIndex != 9 {
		t.Errorf("digit 0: got %d, want 9", p.selectedIndex)
	}

	// Digit beyond list length is ignored.
	short, _, _ := newTestPicker(2, 5)
	short.moveSelection('9')
	if short.selectedIndex != 0 {
		t.Errorf("digit beyond list: got %d, want 0", short.selectedIndex)
	}
}

func TestRenderItemAreaInvokesBuilder(t *testing.T) {
	p, out, calls := newTestPicker(3, 5)
	p.topIndex, p.selectedIndex = 0, 1
	p.hiColorSeq = "\x1b[44m"

	if err := p.renderItemArea(); err != nil {
		t.Fatalf("renderItemArea: %v", err)
	}

	// Builder is called once per visible item with 1-based display index.
	want := []string{"item-0:1", "item-1:2", "item-2:3"}
	if len(*calls) != len(want) {
		t.Fatalf("builder calls: got %v, want %v", *calls, want)
	}
	for i, w := range want {
		if (*calls)[i] != w {
			t.Errorf("call %d: got %q, want %q", i, (*calls)[i], w)
		}
	}

	// The selected row is rendered with the highlight sequence and reset.
	rendered := out.String()
	if !strings.Contains(rendered, "\x1b[44m"+padRight("item-1", 80)+"\x1b[0m") {
		t.Errorf("selected row not highlighted; output: %q", rendered)
	}
	if !strings.Contains(rendered, "item-0") || !strings.Contains(rendered, "item-2") {
		t.Errorf("unselected rows missing; output: %q", rendered)
	}
}

func TestComputeLayout(t *testing.T) {
	p, _, _ := newTestPicker(1, 0)
	p.termHeight = 24
	p.computeLayout(4)
	if p.itemAreaStartRow != 5 || p.separatorRow != 23 || p.hintRow != 24 || p.visibleRows != 18 {
		t.Errorf("layout 24-row: got start=%d sep=%d hint=%d vis=%d",
			p.itemAreaStartRow, p.separatorRow, p.hintRow, p.visibleRows)
	}

	// Tiny terminal: separator pushed below header, visibleRows floored at 3.
	p.termHeight = 5
	p.computeLayout(10)
	if p.separatorRow != 12 || p.visibleRows != 3 {
		t.Errorf("layout tiny: got sep=%d vis=%d, want sep=12 vis=3", p.separatorRow, p.visibleRows)
	}
}

func TestResolveTermDims(t *testing.T) {
	w, h := resolveTermDims(nil, 0, 0)
	if w != 80 || h != 24 {
		t.Errorf("defaults: got %dx%d, want 80x24", w, h)
	}
	w, h = resolveTermDims(nil, 132, 50)
	if w != 132 || h != 50 {
		t.Errorf("explicit: got %dx%d, want 132x50", w, h)
	}
}

package menu

import (
	"sort"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// threadRow is one line of a flattened reply tree: a message the caller may
// read, or a placeholder for a deleted message that a readable one answers.
type threadRow struct {
	Num     int
	Depth   int
	Prefix  string // tree glyphs for the row: "│ " / "  " per level, then "├─" or "└─"
	Deleted bool
	Parent  int                     // the row's parent in the shown tree; 0 for a root
	Msg     *message.DisplayMessage // nil for a deleted placeholder
}

// threadRows flattens the trees rooted at roots, in that order, depth-first
// with children in ascending message order. load returns a message the
// caller may read, deleted or not, or nil for one that is hidden from them
// (another user's private mail, a twit's post): a hidden message is never
// a row, deleted or not, and its replies attach to its nearest shown
// ancestor, or become roots. A deleted message the caller may read is a
// row, as a placeholder, only while a shown message descends from it, so
// that reply keeps its place.
func threadRows(nodes map[int]*message.ReplyNode, roots []int, load func(int) *message.DisplayMessage) []threadRow {
	return newThreadFlattener(nodes, load).rows(roots)
}

// threadForest flattens every thread of the area, newest root first (the
// order the flat list shows messages in), with the rules of threadRows. A
// deleted message is a root only while something readable descends from it.
func threadForest(nodes map[int]*message.ReplyNode, load func(int) *message.DisplayMessage) []threadRow {
	f := newThreadFlattener(nodes, load)
	// The tree can have gaps (an unreadable header is left out), so the
	// keys are walked, not the numbers up to the count.
	nums := make([]int, 0, len(nodes))
	for n := range nodes {
		nums = append(nums, n)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(nums)))
	var roots []int
	for _, n := range nums {
		node := nodes[n]
		if f.shownParent(n) != 0 {
			continue
		}
		if node.Deleted || f.msg(n) != nil {
			if f.hasLive(n) {
				roots = append(roots, n)
			}
		}
	}
	return f.rows(roots)
}

type threadFlattener struct {
	nodes map[int]*message.ReplyNode
	load  func(int) *message.DisplayMessage
	msgs  map[int]*message.DisplayMessage // loaded messages; a nil entry is hidden
	live  map[int]bool                    // hasLive results
}

func newThreadFlattener(nodes map[int]*message.ReplyNode, load func(int) *message.DisplayMessage) *threadFlattener {
	return &threadFlattener{nodes: nodes, load: load, msgs: map[int]*message.DisplayMessage{}, live: map[int]bool{}}
}

// rows flattens the trees under roots, in that order.
func (f *threadFlattener) rows(roots []int) []threadRow {
	var rows []threadRow
	for _, r := range roots {
		rows = f.walk(rows, r, 0, 0, nil)
	}
	return rows
}

// shownParent is n's nearest ancestor that is shown (readable, or a
// deleted placeholder the caller could have read), or 0.
func (f *threadFlattener) shownParent(n int) int {
	p := f.nodes[n].Parent
	for p != 0 && f.msg(p) == nil {
		p = f.nodes[p].Parent
	}
	return p
}

// msg returns n's message, loading it once; nil when n is hidden. A
// deleted message the caller may read is returned too, flagged, so a
// placeholder is drawn only for a deletion the caller could have seen.
func (f *threadFlattener) msg(n int) *message.DisplayMessage {
	if m, ok := f.msgs[n]; ok {
		return m
	}
	var m *message.DisplayMessage
	if f.nodes[n] != nil {
		m = f.load(n)
	}
	f.msgs[n] = m
	return m
}

// hasLive reports whether a readable, undeleted message is n or descends
// from it.
func (f *threadFlattener) hasLive(n int) bool {
	if v, ok := f.live[n]; ok {
		return v
	}
	v := !f.nodes[n].Deleted && f.msg(n) != nil
	for _, c := range f.nodes[n].Children {
		if v {
			break
		}
		v = f.hasLive(c)
	}
	f.live[n] = v
	return v
}

// shownChildren is the children of n in the shown tree: its readable and
// placeholder replies, plus the readable replies hoisted out of hidden
// ones, in ascending order.
func (f *threadFlattener) shownChildren(n int) []int {
	var out []int
	var gather func(int)
	gather = func(p int) {
		for _, c := range f.nodes[p].Children {
			switch {
			case f.msg(c) == nil: // hidden, deleted or not: its replies take its place
				gather(c)
			case f.nodes[c].Deleted:
				if f.hasLive(c) {
					out = append(out, c)
				}
			default:
				out = append(out, c)
			}
		}
	}
	gather(n)
	sort.Ints(out)
	return out
}

// walk appends n and its shown descendants to rows. ancestorsLast[i] says
// whether the ancestor at depth i+1 was the last of its siblings, which
// decides whether a "│ " continues through this row at that level.
func (f *threadFlattener) walk(rows []threadRow, n, depth, parent int, ancestorsLast []bool) []threadRow {
	last := true
	if len(ancestorsLast) > 0 {
		last = ancestorsLast[len(ancestorsLast)-1]
	}
	prefix := ""
	for i := 0; i < depth-1; i++ {
		if ancestorsLast[i] {
			prefix += "  "
		} else {
			prefix += "│ "
		}
	}
	if depth > 0 {
		if last {
			prefix += "└─"
		} else {
			prefix += "├─"
		}
	}
	node := f.nodes[n]
	var m *message.DisplayMessage
	if !node.Deleted {
		m = f.msg(n)
	}
	rows = append(rows, threadRow{Num: n, Depth: depth, Prefix: prefix, Deleted: node.Deleted, Parent: parent, Msg: m})
	children := f.shownChildren(n)
	for i, c := range children {
		rows = f.walk(rows, c, depth+1, n, append(ancestorsLast[:len(ancestorsLast):len(ancestorsLast)], i == len(children)-1))
	}
	return rows
}

// threadView is the reply thread around the message the reader is on, as
// the reader may show it: hidden messages removed, deleted ones kept as
// placeholders. The reader's thread pane and menu read it.
type threadView struct {
	cur   int
	rows  []threadRow
	pos   map[int]int // message number -> index in rows
	nodes map[int]*message.ReplyNode
	load  func(int) *message.DisplayMessage
	msgs  map[int]*message.DisplayMessage
}

// newThreadView builds the view for message cur. nodes is the area's
// ReplyTree; load is as for threadRows. Only the messages of cur's own
// thread are loaded.
func newThreadView(nodes map[int]*message.ReplyNode, cur int, load func(int) *message.DisplayMessage) *threadView {
	if nodes[cur] == nil {
		nodes = map[int]*message.ReplyNode{cur: {Num: cur}}
	}
	tv := &threadView{cur: cur, nodes: nodes, msgs: map[int]*message.DisplayMessage{}, pos: map[int]int{}}
	tv.load = func(n int) *message.DisplayMessage {
		if m, ok := tv.msgs[n]; ok {
			return m
		}
		m := load(n)
		tv.msgs[n] = m
		return m
	}
	// The shown root: climb the parents, stepping over hidden messages.
	root := cur
	for {
		p := tv.shownParent(root)
		if p == 0 {
			break
		}
		root = p
	}
	tv.rows = threadRows(nodes, []int{root}, tv.load)
	for i, r := range tv.rows {
		tv.pos[r.Num] = i
	}
	if _, ok := tv.pos[cur]; !ok {
		// cur is not in its own tree only if load hid it; show it alone.
		tv.rows = []threadRow{{Num: cur, Msg: tv.load(cur)}}
		tv.pos = map[int]int{cur: 0}
	}
	return tv
}

// shownParent is n's nearest ancestor that is shown (readable, or a
// deleted placeholder the caller could have read), or 0.
func (tv *threadView) shownParent(n int) int {
	p := tv.nodes[n].Parent
	for p != 0 && tv.load(p) == nil {
		p = tv.nodes[p].Parent
	}
	return p
}

// row returns n's row, or nil when n is not in the thread.
func (tv *threadView) row(n int) *threadRow {
	if i, ok := tv.pos[n]; ok {
		return &tv.rows[i]
	}
	return nil
}

// order is the thread's readable messages in display order.
func (tv *threadView) order() []int {
	out := make([]int, 0, len(tv.rows))
	for _, r := range tv.rows {
		if !r.Deleted {
			out = append(out, r.Num)
		}
	}
	return out
}

// hasThread reports whether the thread holds any readable message besides
// the current one.
func (tv *threadView) hasThread() bool {
	return len(tv.order()) > 1
}

// directReplies counts the readable replies shown under n.
func (tv *threadView) directReplies(n int) int {
	return len(tv.replies(n))
}

// replies lists the readable replies shown under n, in order.
func (tv *threadView) replies(n int) []int {
	var out []int
	for _, r := range tv.rows {
		if r.Parent == n && !r.Deleted && r.Num != n {
			out = append(out, r.Num)
		}
	}
	return out
}

// position returns n's 1-based place among the thread's readable messages
// and their count.
func (tv *threadView) position(n int) (pos, count int) {
	ord := tv.order()
	for i, m := range ord {
		if m == n {
			return i + 1, len(ord)
		}
	}
	return 0, len(ord)
}

// depth is n's depth in the shown tree.
func (tv *threadView) depth(n int) int {
	if r := tv.row(n); r != nil {
		return r.Depth
	}
	return 0
}

// rootSubject is the subject of the thread's root, or of the first readable
// message when the root is a placeholder.
func (tv *threadView) rootSubject() string {
	for _, r := range tv.rows {
		if r.Msg != nil {
			return r.Msg.Subject
		}
	}
	return ""
}

// threadMenuKey applies key to the thread menu, whose cursor is a message
// number in tv. It reports whether the menu takes the key, and whether the
// key asks to read the message under the cursor (Enter). The arrows, Home
// and End move the cursor over the readable messages, stepping over deleted
// placeholders; T and Esc are taken for the caller to close the menu.
func threadMenuKey(tv *threadView, key int, cursor *int) (handled, read bool) {
	ord := tv.order()
	if len(ord) == 0 {
		return false, false
	}
	i := 0
	for j, n := range ord {
		if n == *cursor {
			i = j
		}
	}
	switch key {
	case editor.KeyArrowUp, editor.KeyCtrlE:
		i = max(i-1, 0)
	case editor.KeyArrowDown, editor.KeyCtrlX:
		i = min(i+1, len(ord)-1)
	case editor.KeyHome:
		i = 0
	case editor.KeyEnd:
		i = len(ord) - 1
	case editor.KeyEnter:
		return true, true
	case editor.KeyEsc, 'T', 't':
		return true, false
	default:
		return false, false
	}
	*cursor = ord[i]
	return true, false
}

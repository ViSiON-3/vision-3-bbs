package menu

import (
	"fmt"
	"sort"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
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
// caller may read, or nil for one that is hidden from them (another user's
// private mail, a twit's post): a hidden message is never a row, and its
// replies attach to its nearest shown ancestor, or become roots. Deleted
// messages are rows, as placeholders, only while a shown message descends
// from them, so that reply keeps its place.
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

// shownParent is n's nearest ancestor that is shown (readable or a deleted
// placeholder), or 0.
func (f *threadFlattener) shownParent(n int) int {
	p := f.nodes[n].Parent
	for p != 0 && !f.nodes[p].Deleted && f.msg(p) == nil {
		p = f.nodes[p].Parent
	}
	return p
}

// msg returns n's message, loading it once; nil when n is hidden or deleted.
func (f *threadFlattener) msg(n int) *message.DisplayMessage {
	if m, ok := f.msgs[n]; ok {
		return m
	}
	var m *message.DisplayMessage
	if node := f.nodes[n]; node != nil && !node.Deleted {
		m = f.load(n)
	}
	f.msgs[n] = m
	return m
}

// hasLive reports whether a shown message is n or descends from it.
func (f *threadFlattener) hasLive(n int) bool {
	if v, ok := f.live[n]; ok {
		return v
	}
	v := f.msg(n) != nil
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
			case f.nodes[c].Deleted:
				if f.hasLive(c) {
					out = append(out, c)
				}
			case f.msg(c) != nil:
				out = append(out, c)
			default: // hidden: its replies take its place
				gather(c)
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
	rows = append(rows, threadRow{Num: n, Depth: depth, Prefix: prefix, Deleted: node.Deleted, Parent: parent, Msg: f.msg(n)})
	children := f.shownChildren(n)
	for i, c := range children {
		rows = f.walk(rows, c, depth+1, n, append(ancestorsLast[:len(ancestorsLast):len(ancestorsLast)], i == len(children)-1))
	}
	return rows
}

// threadView is the reply thread around the message the reader is on, as
// the reader may show it: hidden messages removed, deleted ones kept as
// placeholders. The reader's thread keys, signposts and pane all read it.
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

// shownParent is n's nearest ancestor that is shown (readable or a deleted
// placeholder), or 0.
func (tv *threadView) shownParent(n int) int {
	p := tv.nodes[n].Parent
	for p != 0 && !tv.nodes[p].Deleted && tv.load(p) == nil {
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

// firstReply is the first readable reply shown under n, or 0.
func (tv *threadView) firstReply(n int) int {
	if rs := tv.replies(n); len(rs) > 0 {
		return rs[0]
	}
	return 0
}

// nextSibling is the readable reply after n under the same shown parent, or
// 0. isRoot is set when n is a root of the shown tree.
func (tv *threadView) nextSibling(n int) (next int, isRoot bool) {
	r := tv.row(n)
	if r == nil || r.Parent == 0 {
		return 0, true
	}
	sibs := tv.replies(r.Parent)
	for i, s := range sibs {
		if s == n && i+1 < len(sibs) {
			return sibs[i+1], false
		}
	}
	return 0, false
}

// parentState says what pressing U on n would reach.
type parentState int

const (
	parentNone    parentState = iota // n starts its thread
	parentShown                      // the parent can be read
	parentDeleted                    // the parent is deleted
	parentHidden                     // the parent is not available to this user
)

// parentOf returns n's actual parent and whether the reader may go there.
// Unlike the shown tree, this does not step over hidden messages: a hidden
// parent is reported as such, not replaced by the next ancestor, though it
// is never named.
func (tv *threadView) parentOf(n int) (parent int, state parentState) {
	node := tv.nodes[n]
	if node == nil || node.Parent == 0 {
		return 0, parentNone
	}
	p := node.Parent
	switch {
	case tv.nodes[p].Deleted:
		return p, parentDeleted
	case tv.load(p) == nil:
		return 0, parentHidden
	}
	return p, parentShown
}

// neighbour returns the message before (dir -1) or after (dir +1) n in the
// thread's display order, or 0 at the ends.
func (tv *threadView) neighbour(n, dir int) int {
	ord := tv.order()
	for i, m := range ord {
		if m == n {
			if j := i + dir; j >= 0 && j < len(ord) {
				return ord[j]
			}
			return 0
		}
	}
	return 0
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

// handle is the author of n, for a signpost; "" when n is not readable.
func (tv *threadView) handle(n int) string {
	if m := tv.load(n); m != nil {
		return m.From
	}
	return ""
}

// threadNavTarget resolves one of the reader's thread keys for the message
// tv is on: U (parent), F (first reply), X (next sibling), ] and [ (next and
// previous in thread order). It returns the message to show, or 0 and the
// notice that says why there is none.
func threadNavTarget(tv *threadView, key rune, str *config.StringsConfig) (target int, notice string) {
	cur := tv.cur
	switch key {
	case 'U':
		p, state := tv.parentOf(cur)
		switch state {
		case parentShown:
			return p, ""
		case parentDeleted:
			return 0, fmt.Sprintf(str.MsgThreadParentDeleted, p)
		case parentHidden:
			return 0, str.MsgThreadParentHidden
		}
		return 0, fmt.Sprintf(str.MsgThreadNoParent, cur)
	case 'F':
		if f := tv.firstReply(cur); f != 0 {
			return f, ""
		}
		return 0, fmt.Sprintf(str.MsgThreadNoReplies, cur)
	case 'X':
		next, isRoot := tv.nextSibling(cur)
		switch {
		case next != 0:
			return next, ""
		case isRoot:
			return 0, fmt.Sprintf(str.MsgThreadNoSiblingRoot, cur)
		}
		return 0, fmt.Sprintf(str.MsgThreadNoMoreSiblings, tv.row(cur).Parent)
	case ']':
		if n := tv.neighbour(cur, 1); n != 0 {
			return n, ""
		}
		return 0, str.MsgThreadEnd
	case '[':
		if n := tv.neighbour(cur, -1); n != 0 {
			return n, ""
		}
		return 0, str.MsgThreadStart
	}
	return 0, ""
}

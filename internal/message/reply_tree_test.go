package message

import (
	"reflect"
	"testing"
)

// replyTo posts a reply to parent in area 1, linked by JAM ReplyTo (and the
// parent's MSGID, as the reader's R does).
func replyTo(t *testing.T, mm *MessageManager, areaID, parent int, subject string) int {
	t.Helper()
	parentMsg, err := mm.GetMessage(areaID, parent)
	if err != nil {
		t.Fatalf("GetMessage(%d): %v", parent, err)
	}
	n, err := mm.Post(areaID, "bob", parentMsg.From, subject, "body", PostOptions{
		ReplyToNum: parent, ReplyToMsgID: parentMsg.MsgID,
	})
	if err != nil {
		t.Fatalf("Post reply to %d: %v", parent, err)
	}
	return n
}

// setReplyTo rewrites message n's JAM ReplyTo in place, as a tosser or a
// corrupt base might leave it, and drops the cached tree.
func setReplyTo(t *testing.T, mm *MessageManager, areaID, n, replyTo int) {
	t.Helper()
	b, _, err := mm.openBase(areaID)
	if err != nil {
		t.Fatalf("openBase: %v", err)
	}
	defer func() { _ = b.Close() }()
	hdr, err := b.ReadMessageHeader(n)
	if err != nil {
		t.Fatalf("ReadMessageHeader(%d): %v", n, err)
	}
	hdr.ReplyTo = uint32(replyTo)
	if err := b.UpdateMessageHeader(n, hdr); err != nil {
		t.Fatalf("UpdateMessageHeader(%d): %v", n, err)
	}
	mm.invalidateThreadIndex(areaID)
}

func parentsOf(nodes map[int]*ReplyNode) map[int]int {
	out := make(map[int]int, len(nodes))
	for n, node := range nodes {
		out[n] = node.Parent
	}
	return out
}

// The sample thread from the design handoff: #1 is answered by #3 and #4,
// #3 by #5 and #11, #5 by #7 and #9, #7 by #10, #4 by #8, #8 by #14; #2,
// #6, #12 stand alone and #13 answers #12.
func TestReplyTreeFromReplyTo(t *testing.T) {
	mm := newIndexTestManager(t)
	post(t, mm, 1, "All", "Topaz font in SyncTERM") // 1
	post(t, mm, 1, "All", "Binkd poll schedule")    // 2
	replyTo(t, mm, 1, 1, "Re: Topaz")               // 3
	replyTo(t, mm, 1, 1, "Re: Topaz")               // 4
	replyTo(t, mm, 1, 3, "Re: Topaz")               // 5
	post(t, mm, 1, "All", "Wanted: old .ANS packs") // 6
	replyTo(t, mm, 1, 5, "Re: Topaz")               // 7
	replyTo(t, mm, 1, 4, "Re: Topaz")               // 8
	replyTo(t, mm, 1, 5, "Re: Topaz")               // 9
	replyTo(t, mm, 1, 7, "Re: Topaz")               // 10
	replyTo(t, mm, 1, 3, "Re: Topaz")               // 11
	post(t, mm, 1, "All", "Reddit gateway lag")     // 12
	replyTo(t, mm, 1, 12, "Re: Reddit")             // 13
	replyTo(t, mm, 1, 8, "Re: Topaz")               // 14

	nodes, err := mm.ReplyTree(1)
	if err != nil {
		t.Fatalf("ReplyTree: %v", err)
	}
	wantParents := map[int]int{1: 0, 2: 0, 3: 1, 4: 1, 5: 3, 6: 0, 7: 5, 8: 4, 9: 5, 10: 7, 11: 3, 12: 0, 13: 12, 14: 8}
	if got := parentsOf(nodes); !reflect.DeepEqual(got, wantParents) {
		t.Errorf("parents = %v, want %v", got, wantParents)
	}
	for n, want := range map[int][]int{1: {3, 4}, 3: {5, 11}, 5: {7, 9}, 7: {10}, 4: {8}, 8: {14}, 12: {13}} {
		if got := nodes[n].Children; !reflect.DeepEqual(got, want) {
			t.Errorf("children of #%d = %v, want %v", n, got, want)
		}
	}
	for _, leaf := range []int{2, 6, 9, 10, 11, 13, 14} {
		if len(nodes[leaf].Children) != 0 {
			t.Errorf("#%d should have no children, has %v", leaf, nodes[leaf].Children)
		}
	}
}

// Echomail that arrived with only a REPLY kludge, as a tosser stores it
// before any link pass, is placed by the parent's MSGID.
func TestReplyTreeFallsBackToReplyID(t *testing.T) {
	mm := newIndexTestManager(t)
	first := post(t, mm, 1, "All", "one")
	firstID := msgIDOf(t, mm, 1, first)
	n, err := mm.Post(1, "carol", "alice", "Re: one", "body", PostOptions{ReplyToMsgID: firstID})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if m, _ := mm.GetMessage(1, n); m.ReplyToNum != 0 {
		t.Fatalf("test premise: the reply should carry no JAM ReplyTo, has %d", m.ReplyToNum)
	}

	nodes, err := mm.ReplyTree(1)
	if err != nil {
		t.Fatalf("ReplyTree: %v", err)
	}
	if nodes[n].Parent != first {
		t.Errorf("parent of the REPLY-only message = %d, want %d", nodes[n].Parent, first)
	}
	if got := nodes[first].Children; !reflect.DeepEqual(got, []int{n}) {
		t.Errorf("children of #%d = %v, want [%d]", first, got, n)
	}
}

// A ReplyTo outside the area, or pointing at the message itself, is no link.
func TestReplyTreeIgnoresBadReplyTo(t *testing.T) {
	mm := newIndexTestManager(t)
	a := post(t, mm, 1, "All", "a")
	b := post(t, mm, 1, "All", "b")
	setReplyTo(t, mm, 1, a, 99)
	setReplyTo(t, mm, 1, b, b)

	nodes, err := mm.ReplyTree(1)
	if err != nil {
		t.Fatalf("ReplyTree: %v", err)
	}
	for _, n := range []int{a, b} {
		if nodes[n].Parent != 0 || len(nodes[n].Children) != 0 {
			t.Errorf("#%d = %+v, want an unlinked root", n, nodes[n])
		}
	}
}

// A loop in the ReplyTo links is cut so every message reaches a root.
func TestReplyTreeBreaksCycles(t *testing.T) {
	mm := newIndexTestManager(t)
	a := post(t, mm, 1, "All", "a")
	b := replyTo(t, mm, 1, a, "Re: a")
	c := replyTo(t, mm, 1, b, "Re: a")
	d := replyTo(t, mm, 1, c, "Re: a")
	setReplyTo(t, mm, 1, b, c) // b -> c -> b

	nodes, err := mm.ReplyTree(1)
	if err != nil {
		t.Fatalf("ReplyTree: %v", err)
	}
	// Each message's chain must end at a root in at most len(nodes) steps.
	for n := range nodes {
		cur, steps := n, 0
		for cur != 0 {
			cur = nodes[cur].Parent
			if steps++; steps > len(nodes) {
				t.Fatalf("#%d never reaches a root: %v", n, parentsOf(nodes))
			}
		}
	}
	if nodes[d].Parent != c {
		t.Errorf("the link below the loop was lost: parent of #%d = %d, want %d", d, nodes[d].Parent, c)
	}
	if nodes[a].Parent != 0 {
		t.Errorf("#%d should still be a root", a)
	}
}

// A deleted message that has not been packed away keeps its place, flagged,
// so its reply is still a grandchild of the root.
func TestReplyTreeKeepsDeletedParent(t *testing.T) {
	mm := newIndexTestManager(t)
	root := post(t, mm, 1, "All", "root")
	gone := replyTo(t, mm, 1, root, "Re: root")
	child := replyTo(t, mm, 1, gone, "Re: root")
	if err := mm.DeleteMessage(1, gone); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}

	nodes, err := mm.ReplyTree(1)
	if err != nil {
		t.Fatalf("ReplyTree: %v", err)
	}
	if !nodes[gone].Deleted {
		t.Errorf("#%d should be flagged deleted", gone)
	}
	if nodes[gone].Parent != root || nodes[child].Parent != gone {
		t.Errorf("parents = %v, want %d under %d under %d", parentsOf(nodes), child, gone, root)
	}
}

// The cached tree is rebuilt when the base changes.
func TestReplyTreeRefreshesAfterPost(t *testing.T) {
	mm := newIndexTestManager(t)
	root := post(t, mm, 1, "All", "root")
	before, err := mm.ReplyTree(1)
	if err != nil {
		t.Fatalf("ReplyTree: %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("tree has %d nodes, want 1", len(before))
	}
	reply := replyTo(t, mm, 1, root, "Re: root")
	after, err := mm.ReplyTree(1)
	if err != nil {
		t.Fatalf("ReplyTree: %v", err)
	}
	if after[reply] == nil || after[reply].Parent != root {
		t.Errorf("new reply missing from the rebuilt tree: %v", parentsOf(after))
	}
	if empty, err := mm.ReplyTree(3); err != nil || len(empty) != 0 {
		t.Errorf("empty area: %v, %v; want an empty tree", empty, err)
	}
}

// A tree built while the areas were reloaded is handed back but not cached:
// it may describe a base the reload replaced, which is why the reload
// emptied the cache. The reload runs inside the build-to-publish window
// through the test hook.
func TestReplyTreeBuiltAcrossReloadIsNotCached(t *testing.T) {
	mm := newIndexTestManager(t)
	root := post(t, mm, 1, "All", "one")
	replyTo(t, mm, 1, root, "Re: one")

	reloads := 0
	afterReplyTreeBuild = func() {
		reloads++
		if err := mm.Reload(); err != nil {
			t.Errorf("Reload: %v", err)
		}
	}
	t.Cleanup(func() { afterReplyTreeBuild = nil })

	nodes, err := mm.ReplyTree(1)
	if err != nil {
		t.Fatalf("ReplyTree: %v", err)
	}
	if len(nodes) != 2 || reloads != 1 {
		t.Fatalf("got %d nodes after %d reloads; want the built tree of 2 back", len(nodes), reloads)
	}
	mm.mu.RLock()
	cached := len(mm.replyTrees)
	mm.mu.RUnlock()
	if cached != 0 {
		t.Errorf("a tree built across a reload was cached (%d entries)", cached)
	}

	// With no reload in the window, the next build is cached as usual.
	afterReplyTreeBuild = nil
	if _, err := mm.ReplyTree(1); err != nil {
		t.Fatalf("ReplyTree: %v", err)
	}
	mm.mu.RLock()
	cached = len(mm.replyTrees)
	mm.mu.RUnlock()
	if cached != 1 {
		t.Errorf("an ordinary build should be cached: %d entries", cached)
	}
}

package message

import (
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// ReplyNode is one message in an area's reply tree (ReplyTree).
type ReplyNode struct {
	Num      int
	Parent   int   // 0 = root
	Children []int // ascending message number
	Deleted  bool
}

// afterReplyTreeBuild, when set, runs between building a tree and
// publishing it. Tests use it to reload the areas inside that window.
var afterReplyTreeBuild func()

// replyTree caches an area's reply tree, invalidated like threadIndex.
type replyTree struct {
	total      int
	modCounter uint32
	nodes      map[int]*ReplyNode
}

// ReplyTree returns the area's reply tree, keyed by message number. A
// message's parent is its JAM ReplyTo when that names another message of the
// area, else the message its REPLY kludge names, else none. Reply1st and
// ReplyNext are not consulted: only a link pass fills them, so they are
// often stale. Deleted messages that have not been packed away stay in the
// tree, flagged, so their replies keep their place. A message whose parent
// chain loops is made a root.
//
// The tree is built from headers alone and cached until the base changes.
// The returned map is shared with the cache and must not be modified.
func (mm *MessageManager) ReplyTree(areaID int) (map[int]*ReplyNode, error) {
	// Taken before the base is chosen: a reload at any point after this
	// changes it, and a tree built from the base chosen here is then not
	// cached (it is still returned, being what that base holds).
	mm.mu.RLock()
	gen := mm.areaGen
	mm.mu.RUnlock()

	b, _, err := mm.openBase(areaID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	total, err := b.GetMessageCount()
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return map[int]*ReplyNode{}, nil
	}

	modCounter := uint32(0)
	if mc, mcErr := b.GetModCounter(); mcErr == nil {
		modCounter = mc
	}

	mm.mu.RLock()
	tree := mm.replyTrees[areaID]
	mm.mu.RUnlock()

	stale := func(t *replyTree) bool {
		return t == nil || t.total != total || (modCounter != 0 && t.modCounter != modCounter)
	}
	if stale(tree) {
		// Built without the lock: it reads every header in the area, and
		// nothing else should wait on that. Two readers arriving together
		// may both build; the second keeps whichever fresh tree is stored.
		built := buildReplyTree(b, total, modCounter)
		if afterReplyTreeBuild != nil {
			afterReplyTreeBuild()
		}
		mm.mu.Lock()
		switch cur := mm.replyTrees[areaID]; {
		case mm.areaGen != gen:
			tree = built // the areas changed under us: use it, cache nothing
		case stale(cur):
			mm.replyTrees[areaID] = built
			tree = built
		default:
			tree = cur
		}
		mm.mu.Unlock()
	}
	return tree.nodes, nil
}

// buildReplyTree reads every header of b and links each message to its
// parent. See ReplyTree for the rules.
func buildReplyTree(b *jam.Base, total int, modCounter uint32) *replyTree {
	type link struct {
		replyTo int
		replyID string
	}
	nodes := make(map[int]*ReplyNode, total)
	links := make([]link, total+1)
	// MSGID -> message number, deleted messages included so a reply to one
	// still finds its place. As in buildMSGIDIndex, the address part of an
	// FTN MSGID is indexed too, for REPLY kludges written without the serial.
	msgIDs := make(map[string]int, total)
	for i := 1; i <= total; i++ {
		hdr, err := b.ReadMessageHeader(i)
		if err != nil {
			slog.Warn("failed to read message header for reply tree", "index", i, "error", err)
			continue
		}
		nodes[i] = &ReplyNode{Num: i, Deleted: hdr.Attribute&jam.MsgDeleted != 0}
		l := link{replyTo: int(hdr.ReplyTo)}
		for _, sf := range hdr.Subfields {
			switch sf.LoID {
			case jam.SfldMsgID:
				full := string(sf.Buffer)
				if _, dup := msgIDs[full]; !dup {
					msgIDs[full] = i
				}
				if idx := strings.LastIndex(full, " "); idx > 0 {
					if _, dup := msgIDs[full[:idx]]; !dup {
						msgIDs[full[:idx]] = i
					}
				}
			case jam.SfldReplyID:
				l.replyID = string(sf.Buffer)
			}
		}
		links[i] = l
	}

	for i := 1; i <= total; i++ {
		n := nodes[i]
		if n == nil {
			continue
		}
		p := links[i].replyTo
		if p < 1 || p > total || p == i || nodes[p] == nil {
			p = 0
		}
		if p == 0 && links[i].replyID != "" {
			if q, ok := msgIDs[links[i].replyID]; ok && q != i {
				p = q
			}
		}
		n.Parent = p
	}

	// Break cycles. Walking up the parents from each message, a step onto a
	// message already on the walk closes a loop; the message that took it is
	// made a root. Messages whose walk reached a root are remembered so each
	// is walked once.
	resolved := make(map[int]bool, total)
	for i := 1; i <= total; i++ {
		if nodes[i] == nil || resolved[i] {
			continue
		}
		onPath := map[int]bool{}
		var path []int
		cur := i
		for cur != 0 && !resolved[cur] {
			if onPath[cur] {
				// The last message walked points back into the walk: it
				// is the one cut loose, so the rest keep their links.
				nodes[path[len(path)-1]].Parent = 0
				break
			}
			onPath[cur] = true
			path = append(path, cur)
			cur = nodes[cur].Parent
		}
		for _, n := range path {
			resolved[n] = true
		}
	}

	// Children are appended in ascending message order.
	for i := 1; i <= total; i++ {
		if n := nodes[i]; n != nil && n.Parent != 0 {
			nodes[n.Parent].Children = append(nodes[n.Parent].Children, i)
		}
	}
	return &replyTree{total: total, modCounter: modCounter, nodes: nodes}
}

package message

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// loadMessageAreas loads area definitions from JSON.
func (mm *MessageManager) loadMessageAreas() error {
	data, err := os.ReadFile(mm.areasPath)
	if err != nil {
		return err
	}
	var areasList []*MessageArea
	if len(data) == 0 {
		// An empty file means no areas. Falling through with a nil list
		// (rather than returning early) matters on reload: the maps below
		// must be rebuilt empty, not left holding the previous contents.
		slog.Info("message areas file is empty; none loaded", "path", mm.areasPath)
	} else if err := json.Unmarshal(data, &areasList); err != nil {
		return fmt.Errorf("failed to unmarshal areas from %s: %w", mm.areasPath, err)
	}

	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.recordDiskState(areasList, data)
	mm.installAreasLocked(areasList)
	return nil
}

// recordDiskState keeps the areas as just read from or written to disk: the
// merge base for the next save, and the bytes that tell whether anyone else
// has written the file since. Caller holds mm.mu.
func (mm *MessageManager) recordDiskState(areas []*MessageArea, data []byte) {
	base := make([]MessageArea, 0, len(areas))
	for _, a := range areas {
		if a != nil {
			base = append(base, *a)
		}
	}
	mm.diskBase = CloneAreas(base)
	mm.diskBytes = data
}

// installAreasLocked replaces the in-memory area definitions and rebuilds the
// lookup indexes. Caller holds mm.mu for writing.
func (mm *MessageManager) installAreasLocked(areasList []*MessageArea) {
	mm.areasByID = make(map[int]*MessageArea)
	mm.areasByTag = make(map[string]*MessageArea)
	mm.areasByEchoTag = make(map[string]*MessageArea)
	// The cached indexes are keyed by area ID and validated against the base
	// they were built from; they reset in the SAME critical section as the
	// map swap, or a concurrent lookup could pair the new areas with an index
	// built for a previous BasePath. At construction this is a no-op.
	mm.threadIndex = make(map[int]*threadIndex)
	mm.msgidIndex = make(map[int]*msgidIndex)

	for _, area := range areasList {
		if area == nil {
			continue
		}
		if _, exists := mm.areasByID[area.ID]; exists {
			slog.Warn("duplicate area ID; skipping", "id", area.ID)
			continue
		}
		mm.areasByID[area.ID] = area
		mm.areasByTag[area.Tag] = area
		// Also index by EchoTag for FTN inbound routing when tag-prefix is in use.
		// Existing configs may already contain duplicates, so load stays tolerant
		// and warns rather than failing; AddArea/UpdateAreaByID reject new ones.
		if area.EchoTag != "" && area.EchoTag != area.Tag {
			if prev, dup := mm.areasByEchoTag[area.EchoTag]; dup {
				// Only the last area loaded stays reachable by this echo tag.
				// Same network: its mail goes to that area instead of the other.
				// Different networks: the tosser's network check rejects the
				// mismatch, so the shadowed network's mail goes unrouted.
				slog.Warn("duplicate FTN echo tag; only the last area loaded is reachable by it",
					"echo_tag", area.EchoTag,
					"kept_area", area.Tag, "kept_id", area.ID, "kept_network", area.Network,
					"shadowed_area", prev.Tag, "shadowed_id", prev.ID, "shadowed_network", prev.Network)
			}
			mm.areasByEchoTag[area.EchoTag] = area
		}
		slog.Debug("loaded area", "id", area.ID, "tag", area.Tag, "type", area.AreaType)
	}

	// An area's Tag shadows another area's EchoTag: the tosser tries the tag
	// lookup first and that lookup is not network-gated, so the echo-tagged
	// area never receives anything. Checked after the loop because the two
	// areas can appear in either order.
	for _, area := range mm.areasByID {
		if area.EchoTag == "" || area.EchoTag == area.Tag {
			continue
		}
		if owner, ok := mm.areasByTag[area.EchoTag]; ok && owner.ID != area.ID {
			slog.Warn("FTN echo tag is another area's local tag; mail for it routes to that area instead",
				"echo_tag", area.EchoTag,
				"echo_area", area.Tag, "echo_id", area.ID, "echo_network", area.Network,
				"tag_area", owner.Tag, "tag_id", owner.ID)
		}
	}

	// Two areas on one network whose tags differ only in case both match an
	// inbound tag in the tosser's case-insensitive fallback, and the lower ID
	// wins. AddArea/UpdateAreaByID refuse new ones; warn about any loaded.
	for _, area := range mm.areasByID {
		if other := mm.tagFoldConflict(area.Tag, area.Network, area.ID); other != nil && other.ID < area.ID {
			slog.Warn("area tags on one network differ only in case; mail for them may reach either area",
				"network", area.Network, "tag", area.Tag, "id", area.ID, "other_tag", other.Tag, "other_id", other.ID)
		}
	}

	// Migration: assign positions to any areas that have Position <= 0.
	// Finds the current max position and assigns sequentially after it.
	maxPos := 0
	hasUnset := false
	for _, area := range mm.areasByID {
		if area.Position > maxPos {
			maxPos = area.Position
		}
		if area.Position <= 0 {
			hasUnset = true
		}
	}
	if hasUnset && len(mm.areasByID) > 0 {
		sorted := make([]*MessageArea, 0, len(mm.areasByID))
		for _, area := range mm.areasByID {
			if area.Position <= 0 {
				sorted = append(sorted, area)
			}
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].ID < sorted[j].ID
		})
		for _, area := range sorted {
			maxPos++
			area.Position = maxPos
		}
		slog.Info("auto-assigned message area positions (migration)", "count", len(sorted))
	}
}

// Reload re-reads message_areas.json, replacing the in-memory definitions.
// The cached thread/MSGID indexes drop in the same critical section as the
// map swap (inside loadMessageAreas) — an area's BasePath may have changed,
// and a stale index would answer for the wrong base. JAM bases are
// opened on demand and never cached, so there are no handles to invalidate;
// a message being posted concurrently holds its own area pointer and its own
// open base, and at worst lands in an area removed a moment earlier (the JAM
// files on disk are untouched by a reload). A message_areas.json that fails
// to read or parse leaves the current definitions in place.
//
// Callers must ensure no sysop session is mid-edit in the area editor: its
// whole-file SaveAreas would clobber (or be clobbered by) the sysop's edit.
// The config watcher defers this reload to an idle window, which guarantees
// that. V3Net area bindings are NOT rewired by this reload alone; a
// v3net.json reload (or an area-browser subscription change) rebuilds them
// against the current areas.
func (mm *MessageManager) Reload() error {
	mm.saveMu.Lock()
	defer mm.saveMu.Unlock()
	if err := mm.loadMessageAreas(); err != nil {
		return fmt.Errorf("reloading message areas: %w", err)
	}

	mm.mu.RLock()
	count := len(mm.areasByID)
	mm.mu.RUnlock()

	slog.Info("message areas reloaded", "count", count)
	return nil
}

// openBase opens a JAM base on-demand. The caller must close it when done.
// This method does not hold any locks and should be called after releasing mm.mu.
// Returns ErrAreaNotFound if the area doesn't exist.
func (mm *MessageManager) openBase(areaID int) (*jam.Base, *MessageArea, error) {
	mm.mu.RLock()
	area, exists := mm.areasByID[areaID]
	mm.mu.RUnlock()

	if !exists {
		return nil, nil, ErrAreaNotFound
	}

	basePath := mm.resolveBasePath(area)
	b, err := jam.Open(basePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open JAM base for area %d: %w", areaID, err)
	}

	return b, area, nil
}

// resolveBasePath returns the absolute path for a JAM base.
func (mm *MessageManager) resolveBasePath(area *MessageArea) string {
	bp := area.BasePath
	if bp == "" {
		bp = "msgbases/" + strings.ToLower(area.Tag)
	}
	if filepath.IsAbs(bp) {
		return bp
	}
	return filepath.Join(mm.dataPath, bp)
}

// SaveAreas persists all message areas to message_areas.json atomically.
//
// Other programs write the file too (the config editor, helper), so the save
// holds the cross-process areas lock and, if the file changed since this
// manager last read or wrote it, folds those changes in with MergeAreas rather
// than overwriting them. The merged result then replaces the in-memory areas,
// so the BBS sees the other writer's change without waiting for the config
// watcher's reload.
func (mm *MessageManager) SaveAreas() error {
	_, err := mm.saveAreas()
	return err
}

// saveAreas is SaveAreas, also returning any IDs the merge renumbered (see
// AreaMergeResult.Renumbered).
func (mm *MessageManager) saveAreas() (map[int]int, error) {
	mm.saveMu.Lock()
	defer mm.saveMu.Unlock()

	// If the lock cannot be taken, save anyway rather than lose the change:
	// that is the old, unlocked behaviour, and the merge below still catches
	// any write that landed before ours.
	lock, lockErr := LockAreasFile(mm.areasPath)
	if lockErr != nil {
		slog.Warn("saving message areas without the cross-process lock; a concurrent write could be lost",
			"path", mm.areasPath, "error", lockErr)
	}
	defer lock.Release() // nil-safe

	// Held across read, merge, write, and install so an update made between
	// the snapshot and the install cannot be dropped from memory.
	mm.mu.Lock()
	defer mm.mu.Unlock()

	ours := make([]MessageArea, 0, len(mm.areasByID))
	for _, area := range mm.areasByID {
		if area != nil {
			ours = append(ours, *area)
		}
	}
	ours = CloneAreas(ours)

	out := ours
	var renumbered map[int]int
	external := false
	disk, data, readErr := ReadAreasFile(mm.areasPath)
	switch {
	case readErr != nil:
		// An unreadable file has nothing to merge; replacing it is what a
		// save did before merging existed.
		slog.Warn("could not read message areas before saving; overwriting", "path", mm.areasPath, "error", readErr)
	case data == nil && mm.diskBytes != nil:
		// The file vanished. Merging would read that as every area deleted
		// elsewhere; write ours back instead, as a save always has.
		slog.Warn("message areas file is missing; recreating it from memory", "path", mm.areasPath)
	case !bytes.Equal(data, mm.diskBytes):
		res, err := MergeAreas(mm.diskBase, ours, disk)
		if err != nil {
			return nil, fmt.Errorf("merging message areas: %w", err)
		}
		for _, c := range res.Conflicts {
			slog.Warn("message areas merge conflict", "detail", c.What)
		}
		slog.Info("merged message area changes made outside the BBS", "path", mm.areasPath)
		out, renumbered, external = res.Areas, res.Renumbered, true
	}

	written, err := WriteAreasFile(mm.areasPath, out)
	if err != nil {
		return nil, fmt.Errorf("failed to save message areas: %w", err)
	}

	ptrs := make([]*MessageArea, len(out))
	for i := range out {
		a := out[i]
		ptrs[i] = &a
	}
	mm.recordDiskState(ptrs, written)
	if external {
		mm.installAreasLocked(ptrs)
	}
	return renumbered, nil
}

// PackAndLinkArea packs the JAM base for the given area (removing deleted
// messages and renumbering) then rebuilds reply threading chains. Caches
// are invalidated afterward.
func (mm *MessageManager) PackAndLinkArea(areaID int) error {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return fmt.Errorf("open base for area %d: %w", areaID, err)
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()
	if _, err := b.Pack(); err != nil {
		return fmt.Errorf("pack area %d: %w", areaID, err)
	}
	if _, err := b.Link(); err != nil {
		return fmt.Errorf("link area %d: %w", areaID, err)
	}
	mm.invalidateThreadIndex(areaID)
	return nil
}

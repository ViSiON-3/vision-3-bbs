package message

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// GetAreaByID retrieves a message area by its ID.
func (mm *MessageManager) GetAreaByID(id int) (*MessageArea, bool) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	area, exists := mm.areasByID[id]
	return area, exists
}

// GetAreaByTag retrieves a message area by its tag.
func (mm *MessageManager) GetAreaByTag(tag string) (*MessageArea, bool) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	area, exists := mm.areasByTag[tag]
	return area, exists
}

// echoTagConflict reports an area that already claims echoTag, excluding
// excludeID. Two kinds of claim count:
//
//   - Another area with the same EchoTag on the same network. areasByEchoTag
//     keeps only the last writer, so that echo's mail silently lands in
//     whichever area was indexed last. Areas on DIFFERENT networks may
//     legitimately share an echo tag -- the same echo name exists on separate
//     FTN networks -- and the tosser disambiguates by network when routing
//     (internal/tosser/import.go).
//
//   - Any area whose Tag equals echoTag exactly, on any network. The tosser
//     tries GetAreaByTag first and that lookup is NOT network-gated, so a tag
//     match always wins and the echo-tagged area would never receive anything.
//
// On the same network the comparison also ignores case, for both kinds: the
// tosser falls back to a case-insensitive match on its own network
// (FindEchoAreaFold), so two tags there differing only in case would compete
// for the same inbound mail. Caller must hold mm.mu.
func (mm *MessageManager) echoTagConflict(echoTag, network string, excludeID int) *MessageArea {
	if echoTag == "" {
		return nil
	}
	for _, a := range mm.areasByID {
		if a.ID == excludeID {
			continue
		}
		if a.Tag == echoTag {
			return a
		}
		if !strings.EqualFold(a.Network, network) {
			continue
		}
		if strings.EqualFold(a.Tag, echoTag) || (a.EchoTag != "" && strings.EqualFold(a.EchoTag, echoTag)) {
			return a
		}
	}
	return nil
}

// tagFoldConflict reports another area on network whose Tag or EchoTag equals
// tag ignoring case, excluding excludeID. FindEchoAreaFold matches local tags
// on the tosser's network as well as echo tags, so "FOO" and "foo" there would
// compete for an inbound "AREA: FoO". Areas with no network are never tossed
// to by the fallback and are not checked. Caller must hold mm.mu.
func (mm *MessageManager) tagFoldConflict(tag, network string, excludeID int) *MessageArea {
	if tag == "" || network == "" {
		return nil
	}
	for _, a := range mm.areasByID {
		if a.ID == excludeID || !strings.EqualFold(a.Network, network) {
			continue
		}
		if strings.EqualFold(a.Tag, tag) || (a.EchoTag != "" && strings.EqualFold(a.EchoTag, tag)) {
			return a
		}
	}
	return nil
}

// echoTagConflictError describes why echoTag cannot be used, naming the area
// that already claims it and how.
func echoTagConflictError(echoTag string, c *MessageArea) error {
	if strings.EqualFold(c.Tag, echoTag) {
		return fmt.Errorf("echo tag %q is the local tag of area %q (id %d); inbound mail for it already routes there",
			echoTag, c.Tag, c.ID)
	}
	return fmt.Errorf("echo tag %q already used by area %q (id %d) on network %q",
		echoTag, c.Tag, c.ID, c.Network)
}

// tagFoldConflictError describes why tag cannot be used on its network.
func tagFoldConflictError(tag string, c *MessageArea) error {
	return fmt.Errorf("tag %q differs only in case from area %q (id %d) on network %q; inbound mail could reach either",
		tag, c.Tag, c.ID, c.Network)
}

// GetAreaByEchoTag retrieves a message area by its FTN echo tag.
// Used when areas have a local tag-prefix (e.g. Tag="FD_LINUX", EchoTag="LINUX").
func (mm *MessageManager) GetAreaByEchoTag(echoTag string) (*MessageArea, bool) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	area, exists := mm.areasByEchoTag[echoTag]
	return area, exists
}

// FindEchoAreaFold is the tosser's last resort for an echo tag that matched no
// area exactly. FTN echo tags are case-insensitive by convention, and hubs do
// send them in lower case ("0n-warez") while area lists and wizards write
// them in upper case, so an exact-only lookup turns every such message into
// "unknown area".
//
// Only areas on network are considered, so a case variant of another
// network's tag reports an unknown area rather than tossing the message into
// that network's base. An area whose EchoTag (or Tag, when it has no EchoTag)
// equals tag ignoring case wins; failing that, one whose local Tag does. When
// several areas qualify at the same step, the lowest ID wins so routing is
// stable across restarts.
func (mm *MessageManager) FindEchoAreaFold(tag, network string) (*MessageArea, bool) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	var byEcho, byTag *MessageArea
	lower := func(cur, a *MessageArea) *MessageArea {
		if cur == nil || a.ID < cur.ID {
			return a
		}
		return cur
	}
	for _, a := range mm.areasByID {
		if !strings.EqualFold(a.Network, network) {
			continue
		}
		echo := a.EchoTag
		if echo == "" {
			echo = a.Tag
		}
		if strings.EqualFold(echo, tag) {
			byEcho = lower(byEcho, a)
		} else if strings.EqualFold(a.Tag, tag) {
			byTag = lower(byTag, a)
		}
	}
	if byEcho != nil {
		return byEcho, true
	}
	return byTag, byTag != nil
}

// UpdateAreaByID replaces the message area with the given ID with a copy of updated.
// Callers must not modify areas by writing through pointers returned from GetAreaByID;
// use this method so the update is performed under the manager's lock and avoids races.
// Returns ErrAreaNotFound if no area has the given ID. The updated area's ID must match id.
func (mm *MessageManager) UpdateAreaByID(id int, updated MessageArea) error {
	if updated.ID != id {
		return fmt.Errorf("message area ID mismatch: got %d, want %d", updated.ID, id)
	}
	mm.mu.Lock()
	defer mm.mu.Unlock()
	old, exists := mm.areasByID[id]
	if !exists {
		return ErrAreaNotFound
	}
	oldTag := old.Tag
	oldEchoTag := old.EchoTag
	replacement := new(MessageArea)
	*replacement = updated
	// Validate everything before touching any index, so a rejected update
	// leaves the manager exactly as it was.
	if oldTag != updated.Tag {
		if existing, ok := mm.areasByTag[updated.Tag]; ok && existing.ID != id {
			return fmt.Errorf("tag %q already in use by area %d", updated.Tag, existing.ID)
		}
	}
	// Checked only when the tags or network change, so an area already
	// loaded beside a case variant can still be edited for other reasons;
	// LoadAreas warns about those.
	moved := !strings.EqualFold(old.Network, updated.Network)
	if moved || oldEchoTag != updated.EchoTag {
		if conflict := mm.echoTagConflict(updated.EchoTag, updated.Network, id); conflict != nil {
			return echoTagConflictError(updated.EchoTag, conflict)
		}
	}
	if moved || !strings.EqualFold(oldTag, updated.Tag) {
		if conflict := mm.tagFoldConflict(updated.Tag, updated.Network, id); conflict != nil {
			return tagFoldConflictError(updated.Tag, conflict)
		}
	}
	if oldTag != updated.Tag {
		delete(mm.areasByTag, oldTag)
	}

	// Keep areasByEchoTag in sync when EchoTag changes.
	if oldEchoTag != "" && oldEchoTag != old.Tag {
		delete(mm.areasByEchoTag, oldEchoTag)
	}
	if updated.EchoTag != "" && updated.EchoTag != updated.Tag {
		mm.areasByEchoTag[updated.EchoTag] = replacement
	}
	mm.areasByID[id] = replacement
	mm.areasByTag[updated.Tag] = replacement
	return nil
}

// AddArea inserts a new message area, auto-assigning the next available ID
// and Position. The area's Tag must be unique. After insertion the area list
// is persisted to disk. Returns the assigned ID.
func (mm *MessageManager) AddArea(area MessageArea) (int, error) {
	mm.mu.Lock()

	// Check tag uniqueness.
	if _, exists := mm.areasByTag[area.Tag]; exists {
		mm.mu.Unlock()
		return 0, fmt.Errorf("message area tag %q already exists", area.Tag)
	}

	if conflict := mm.echoTagConflict(area.EchoTag, area.Network, 0); conflict != nil {
		mm.mu.Unlock()
		return 0, echoTagConflictError(area.EchoTag, conflict)
	}
	if conflict := mm.tagFoldConflict(area.Tag, area.Network, 0); conflict != nil {
		mm.mu.Unlock()
		return 0, tagFoldConflictError(area.Tag, conflict)
	}

	// Assign next ID and position.
	maxID := 0
	maxPos := 0
	for _, a := range mm.areasByID {
		if a.ID > maxID {
			maxID = a.ID
		}
		if a.Position > maxPos {
			maxPos = a.Position
		}
	}
	area.ID = maxID + 1
	area.Position = maxPos + 1

	// Default base path if empty.
	if area.BasePath == "" {
		area.BasePath = fmt.Sprintf("msgbases/area_%d", area.ID)
	}

	ptr := new(MessageArea)
	*ptr = area
	mm.areasByID[area.ID] = ptr
	mm.areasByTag[area.Tag] = ptr
	if area.EchoTag != "" && area.EchoTag != area.Tag {
		mm.areasByEchoTag[area.EchoTag] = ptr
	}
	mm.mu.Unlock()

	slog.Info("auto-created message area", "id", area.ID, "tag", area.Tag, "type", area.AreaType)

	if err := mm.SaveAreas(); err != nil {
		// Rollback in-memory state so it stays consistent with disk.
		mm.mu.Lock()
		delete(mm.areasByID, area.ID)
		delete(mm.areasByTag, area.Tag)
		if area.EchoTag != "" && area.EchoTag != area.Tag {
			delete(mm.areasByEchoTag, area.EchoTag)
		}
		mm.mu.Unlock()
		slog.Error("rolling back area after save failure", "tag", area.Tag, "error", err)
		return 0, fmt.Errorf("save areas after add: %w", err)
	}
	return area.ID, nil
}

// ListAreas returns all loaded areas sorted by Position.
func (mm *MessageManager) ListAreas() []*MessageArea {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	list := make([]*MessageArea, 0, len(mm.areasByID))
	for _, area := range mm.areasByID {
		list = append(list, area)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Position != list[j].Position {
			return list[i].Position < list[j].Position
		}
		return list[i].ID < list[j].ID
	})
	return list
}

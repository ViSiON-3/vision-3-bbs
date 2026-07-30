package message

import (
	"sort"
)

// MoveAreaPosition moves the area with the given ID to newPosition,
// shifting other areas as needed and renumbering all positions sequentially.
func (mm *MessageManager) MoveAreaPosition(areaID int, newPosition int) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	area, exists := mm.areasByID[areaID]
	if !exists {
		return ErrAreaNotFound
	}

	// Build sorted list of all areas by current position
	areas := make([]*MessageArea, 0, len(mm.areasByID))
	for _, a := range mm.areasByID {
		areas = append(areas, a)
	}
	sort.Slice(areas, func(i, j int) bool {
		return areas[i].Position < areas[j].Position
	})

	// Remove the area from its current position
	var filtered []*MessageArea
	for _, a := range areas {
		if a.ID != area.ID {
			filtered = append(filtered, a)
		}
	}

	// Clamp newPosition to valid range (1-based)
	if newPosition < 1 {
		newPosition = 1
	}
	insertIdx := newPosition - 1
	if insertIdx > len(filtered) {
		insertIdx = len(filtered)
	}

	// Insert at new position
	result := make([]*MessageArea, 0, len(filtered)+1)
	result = append(result, filtered[:insertIdx]...)
	result = append(result, area)
	result = append(result, filtered[insertIdx:]...)

	// Renumber sequentially
	for i, a := range result {
		a.Position = i + 1
	}

	return nil
}

// MoveAreaPositionInConference reorders an area within its conference.
// newIndex is 1-based within the conference's area list. Only areas in
// the same conference are rearranged; areas in other conferences keep
// their relative positions.
func (mm *MessageManager) MoveAreaPositionInConference(areaID int, newIndex int) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	area, exists := mm.areasByID[areaID]
	if !exists {
		return ErrAreaNotFound
	}
	confID := area.ConferenceID

	// Build sorted list of ALL areas by current position
	all := make([]*MessageArea, 0, len(mm.areasByID))
	for _, a := range mm.areasByID {
		all = append(all, a)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].Position < all[j].Position
	})

	// Extract areas in this conference (preserving order)
	var confAreas []*MessageArea
	for _, a := range all {
		if a.ConferenceID == confID {
			confAreas = append(confAreas, a)
		}
	}

	// Remove the source area from the conference list
	var filtered []*MessageArea
	for _, a := range confAreas {
		if a.ID != areaID {
			filtered = append(filtered, a)
		}
	}

	// Clamp newIndex to valid range (1-based)
	if newIndex < 1 {
		newIndex = 1
	}
	insertIdx := newIndex - 1
	if insertIdx > len(filtered) {
		insertIdx = len(filtered)
	}

	// Insert at new position within conference
	reordered := make([]*MessageArea, 0, len(filtered)+1)
	reordered = append(reordered, filtered[:insertIdx]...)
	reordered = append(reordered, area)
	reordered = append(reordered, filtered[insertIdx:]...)

	// Reassemble: interleave the reordered conference areas back into
	// the global list at the same slots they originally occupied.
	result := make([]*MessageArea, 0, len(all))
	confIdx := 0
	for _, a := range all {
		if a.ConferenceID == confID {
			result = append(result, reordered[confIdx])
			confIdx++
		} else {
			result = append(result, a)
		}
	}

	// Renumber all positions sequentially
	for i, a := range result {
		a.Position = i + 1
	}

	return nil
}

package message

import (
	"log/slog"
	"strings"
)

// BindSponsors gives each area whose Sponsor has no account ID yet the ID
// of the account holding that handle, as idFor returns it, or NoSponsorID
// when idFor finds none (0 or less), and saves the areas when any changed.
// The BBS runs it whenever it loads the areas: at startup for areas from
// before IDs, and after a reload for a sponsor set by name in the config
// editor or helper, which clear the ID. Once bound, only the ID counts, so a
// purged or renamed sponsor's area can't pass to whoever takes the old
// handle (#1020). It reports how many it bound to an account and to nobody.
//
// A sponsor already bound keeps its account; when nameFor reports that the
// account's handle has since changed (a rename), the stored name is updated
// to it (#1052), so the file, the Sponsor Menu and doctor show who the
// sponsor is now. nameFor may be nil.
//
// If the save fails, the areas are put back as they were in memory too, so
// the running BBS agrees with the file: no sponsor until the binding is
// saved.
func (mm *MessageManager) BindSponsors(idFor func(handle string) int, nameFor func(id int) (string, bool)) (bound, unowned int, err error) {
	var changed []MessageArea // the areas as they were before binding
	defer func() {
		if err == nil {
			return
		}
		for _, before := range changed {
			if cur, ok := mm.GetAreaByID(before.ID); ok {
				undo := *cur
				undo.SponsorID, undo.Sponsor = before.SponsorID, before.Sponsor
				if uerr := mm.UpdateAreaByID(before.ID, undo); uerr != nil {
					slog.Error("putting an area's sponsor back unbound", "area", before.Tag, "error", uerr)
				}
			}
		}
		bound, unowned = 0, 0
	}()
	renamed := 0
	for _, a := range mm.ListAreas() {
		if a == nil || a.Sponsor == "" {
			continue
		}
		if a.SponsorID > 0 {
			// Bound: follow a rename of the account.
			if nameFor == nil {
				continue
			}
			name, ok := nameFor(a.SponsorID)
			if !ok || name == "" || strings.EqualFold(name, a.Sponsor) {
				continue
			}
			updated := *a
			changed = append(changed, *a)
			updated.Sponsor = name
			renamed++
			slog.Info("area sponsor's name follows a rename of its account", "area", a.Tag, "was", a.Sponsor, "now", name, "id", a.SponsorID)
			if uerr := mm.UpdateAreaByID(a.ID, updated); uerr != nil {
				return bound, unowned, uerr
			}
			continue
		}
		if a.SponsorID != 0 {
			continue
		}
		updated := *a
		changed = append(changed, *a)
		if id := idFor(a.Sponsor); id > 0 {
			updated.SponsorID = id
			bound++
			slog.Info("area sponsor bound to an account", "area", a.Tag, "sponsor", a.Sponsor, "id", id)
		} else {
			updated.SponsorID = NoSponsorID
			unowned++
			slog.Warn("area sponsor names no account, so the area has no sponsor until it is set again", "area", a.Tag, "sponsor", a.Sponsor)
		}
		if uerr := mm.UpdateAreaByID(a.ID, updated); uerr != nil {
			return bound, unowned, uerr
		}
	}
	if bound+unowned+renamed > 0 {
		err = mm.SaveAreas()
	}
	return bound, unowned, err
}

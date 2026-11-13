package message

import "log/slog"

// BindSponsors gives each area whose Sponsor has no account ID yet the ID
// of the account holding that handle, as idFor returns it, or NoSponsorID
// when idFor finds none (0 or less), and saves the areas when any changed.
// The BBS runs it whenever it loads the areas: at startup for areas from
// before IDs, and after a reload for a sponsor set by name in the config
// editor or helper, which clear the ID. Once bound, only the ID counts, so a
// purged or renamed sponsor's area can't pass to whoever takes the old
// handle (#1020). It reports how many it bound to an account and to nobody.
//
// If the save fails, the areas are put back unbound in memory too, so the
// running BBS agrees with the file: no sponsor until the binding is saved.
func (mm *MessageManager) BindSponsors(idFor func(handle string) int) (bound, unowned int, err error) {
	var changed []MessageArea // the areas as they were before binding
	defer func() {
		if err == nil {
			return
		}
		for _, before := range changed {
			if cur, ok := mm.GetAreaByID(before.ID); ok {
				undo := *cur
				undo.SponsorID = before.SponsorID
				if uerr := mm.UpdateAreaByID(before.ID, undo); uerr != nil {
					slog.Error("putting an area's sponsor back unbound", "area", before.Tag, "error", uerr)
				}
			}
		}
		bound, unowned = 0, 0
	}()
	for _, a := range mm.ListAreas() {
		if a == nil || a.Sponsor == "" || a.SponsorID != 0 {
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
	if bound+unowned > 0 {
		err = mm.SaveAreas()
	}
	return bound, unowned, err
}

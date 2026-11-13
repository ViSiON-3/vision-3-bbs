package file

import (
	"log/slog"
	"sort"
)

// BindUploaders gives each record that has no uploader ID yet the ID of the
// account whose handle is in UploadedBy, as idFor returns it, or
// NoUploaderID when idFor finds none (idFor returns 0 or less). It runs at
// startup: records written before IDs were kept are bound once, to the
// account that holds the handle now, and from then on only the ID counts,
// so a purged or renamed account's uploads can't pass to whoever takes its
// old handle (#1020). Records already bound are left alone. It reports how
// many records it bound to an account and how many to nobody.
func (fm *FileManager) BindUploaders(idFor func(handle string) int) (bound, unowned int, err error) {
	var areaIDs []int
	for _, a := range fm.ListAreas() {
		for _, r := range fm.GetFilesForArea(a.ID) {
			if r.UploadedByID == 0 {
				areaIDs = append(areaIDs, a.ID)
				break
			}
		}
	}
	sort.Ints(areaIDs)
	for _, areaID := range areaIDs {
		merr := fm.mutateAreas([]int{areaID}, func(lists map[int][]FileRecord) (func(), error) {
			records := lists[areaID]
			for i := range records {
				if records[i].UploadedByID != 0 {
					continue
				}
				if id := idFor(records[i].UploadedBy); id > 0 {
					records[i].UploadedByID = id
					bound++
				} else {
					records[i].UploadedByID = NoUploaderID
					unowned++
				}
			}
			return nil, nil
		})
		if merr != nil {
			slog.Error("binding uploaders to accounts", "area", areaID, "error", merr)
			err = merr
		}
	}
	return bound, unowned, err
}

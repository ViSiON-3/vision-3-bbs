package scripting

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// registerFile creates the v3.file object for file area access. Only the
// areas the caller may list (Providers.CanList) are seen: the others, and
// their files, are left out as the file menus leave them out.
func registerFile(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()
	mgr := eng.providers.FileMgr

	// areas() — the file areas the caller may list [{id, tag, name, description}].
	jsutil.Set(obj, "areas", func(call goja.FunctionCall) goja.Value {
		arr := vm.NewArray()
		i := 0
		for _, a := range mgr.ListAreas() {
			if eng.canList(&a) {
				jsutil.Set(arr, itoa(i), fileAreaToJS(vm, &a))
				i++
			}
		}
		return arr
	})

	// area(tag) — get a single area by tag, returns object or null.
	jsutil.Set(obj, "area", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Null()
		}
		tag := call.Arguments[0].String()
		a, found := mgr.GetAreaByTag(tag)
		if !found || !eng.canList(a) {
			return goja.Null()
		}
		return fileAreaToJS(vm, a)
	})

	// list(areaID) — files in an area.
	jsutil.Set(obj, "list", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.NewArray()
		}
		areaID := int(call.Arguments[0].ToInteger())
		if !eng.canListID(areaID) {
			return vm.NewArray()
		}
		files := mgr.GetFilesForArea(areaID)
		arr := vm.NewArray()
		for i, f := range files {
			jsutil.Set(arr, itoa(i), fileRecordToJS(vm, &f))
		}
		return arr
	})

	// count(areaID) — file count in an area.
	jsutil.Set(obj, "count", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(0)
		}
		areaID := int(call.Arguments[0].ToInteger())
		if !eng.canListID(areaID) {
			return vm.ToValue(0)
		}
		count, err := mgr.GetFileCountForArea(areaID)
		if err != nil {
			return vm.ToValue(0)
		}
		return vm.ToValue(count)
	})

	// search(query) — keyword search across the areas the caller may list.
	jsutil.Set(obj, "search", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.NewArray()
		}
		query := call.Arguments[0].String()
		listable := map[int]bool{}
		arr := vm.NewArray()
		i := 0
		for _, f := range mgr.SearchFiles(query) {
			ok, seen := listable[f.AreaID]
			if !seen {
				ok = eng.canListID(f.AreaID)
				listable[f.AreaID] = ok
			}
			if ok {
				jsutil.Set(arr, itoa(i), fileRecordToJS(vm, &f))
				i++
			}
		}
		return arr
	})

	// totalCount() — total files across the areas the caller may list.
	jsutil.Set(obj, "totalCount", func(call goja.FunctionCall) goja.Value {
		total := 0
		for _, a := range mgr.ListAreas() {
			if !eng.canList(&a) {
				continue
			}
			if n, err := mgr.GetFileCountForArea(a.ID); err == nil {
				total += n
			}
		}
		return vm.ToValue(total)
	})

	registerFileRead(obj, eng)
	registerFileMark(obj, eng)
	jsutil.Set(v3, "file", obj)
}

// canList reports whether the caller may see area a (Providers.CanList).
func (eng *Engine) canList(a *file.FileArea) bool {
	can := eng.providers.CanList
	return can != nil && can(a)
}

// canListID is canList for an area named by its ID; an unknown area is not
// visible.
func (eng *Engine) canListID(id int) bool {
	a, ok := eng.providers.FileMgr.GetAreaByID(id)
	return ok && eng.canList(a)
}

func fileAreaToJS(vm *goja.Runtime, a *file.FileArea) goja.Value {
	obj := vm.NewObject()
	jsutil.Set(obj, "id", a.ID)
	jsutil.Set(obj, "tag", a.Tag)
	jsutil.Set(obj, "name", a.Name)
	jsutil.Set(obj, "description", a.Description)
	jsutil.Set(obj, "conferenceID", a.ConferenceID)
	return obj
}

func fileRecordToJS(vm *goja.Runtime, f *file.FileRecord) goja.Value {
	obj := vm.NewObject()
	jsutil.Set(obj, "id", f.ID.String())
	jsutil.Set(obj, "areaID", f.AreaID)
	jsutil.Set(obj, "filename", f.Filename)
	jsutil.Set(obj, "description", f.Description)
	jsutil.Set(obj, "size", f.Size)
	jsutil.Set(obj, "uploadedAt", f.UploadedAt.Unix())
	jsutil.Set(obj, "uploadedBy", f.UploadedBy)
	jsutil.Set(obj, "downloadCount", f.DownloadCount)
	return obj
}

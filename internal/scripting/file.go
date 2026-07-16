package scripting

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// registerFile creates the v3.file object for file area access.
func registerFile(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()
	mgr := eng.providers.FileMgr

	// areas() — list all file areas [{id, tag, name, description}].
	jsutil.Set(obj, "areas", func(call goja.FunctionCall) goja.Value {
		areas := mgr.ListAreas()
		arr := vm.NewArray()
		for i, a := range areas {
			jsutil.Set(arr, itoa(i), fileAreaToJS(vm, &a))
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
		if !found {
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
		count, err := mgr.GetFileCountForArea(areaID)
		if err != nil {
			return vm.ToValue(0)
		}
		return vm.ToValue(count)
	})

	// search(query) — keyword search across all areas.
	jsutil.Set(obj, "search", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.NewArray()
		}
		query := call.Arguments[0].String()
		files := mgr.SearchFiles(query)
		arr := vm.NewArray()
		for i, f := range files {
			jsutil.Set(arr, itoa(i), fileRecordToJS(vm, &f))
		}
		return arr
	})

	// totalCount() — total files across all areas.
	jsutil.Set(obj, "totalCount", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(mgr.GetTotalFileCount())
	})

	jsutil.Set(v3, "file", obj)
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

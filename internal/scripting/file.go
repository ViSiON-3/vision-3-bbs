package scripting

import (
	"github.com/dop251/goja"
	"github.com/stlalpha/vision3/internal/file"
)

// registerFile creates the v3.file object for file area access.
func registerFile(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()
	mgr := eng.providers.FileMgr

	// areas() — list all file areas [{id, tag, name, description}].
	obj.Set("areas", func(call goja.FunctionCall) goja.Value {
		areas := mgr.ListAreas()
		arr := vm.NewArray()
		for i, a := range areas {
			arr.Set(itoa(i), fileAreaToJS(vm, &a))
		}
		return arr
	})

	// area(tag) — get a single area by tag, returns object or null.
	obj.Set("area", func(call goja.FunctionCall) goja.Value {
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
	obj.Set("list", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.NewArray()
		}
		areaID := int(call.Arguments[0].ToInteger())
		files := mgr.GetFilesForArea(areaID)
		arr := vm.NewArray()
		for i, f := range files {
			arr.Set(itoa(i), fileRecordToJS(vm, &f))
		}
		return arr
	})

	// count(areaID) — file count in an area.
	obj.Set("count", func(call goja.FunctionCall) goja.Value {
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
	obj.Set("search", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.NewArray()
		}
		query := call.Arguments[0].String()
		files := mgr.SearchFiles(query)
		arr := vm.NewArray()
		for i, f := range files {
			arr.Set(itoa(i), fileRecordToJS(vm, &f))
		}
		return arr
	})

	// totalCount() — total files across all areas.
	obj.Set("totalCount", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(mgr.GetTotalFileCount())
	})

	v3.Set("file", obj)
}

func fileAreaToJS(vm *goja.Runtime, a *file.FileArea) goja.Value {
	obj := vm.NewObject()
	obj.Set("id", a.ID)
	obj.Set("tag", a.Tag)
	obj.Set("name", a.Name)
	obj.Set("description", a.Description)
	obj.Set("conferenceID", a.ConferenceID)
	return obj
}

func fileRecordToJS(vm *goja.Runtime, f *file.FileRecord) goja.Value {
	obj := vm.NewObject()
	obj.Set("id", f.ID.String())
	obj.Set("areaID", f.AreaID)
	obj.Set("filename", f.Filename)
	obj.Set("description", f.Description)
	obj.Set("size", f.Size)
	obj.Set("uploadedAt", f.UploadedAt.Unix())
	obj.Set("uploadedBy", f.UploadedBy)
	obj.Set("downloadCount", f.DownloadCount)
	return obj
}

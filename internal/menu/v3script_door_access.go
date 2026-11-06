package menu

import "github.com/ViSiON-3/vision-3-bbs/internal/file"

// v3ScriptCanDownload is the CanDownload a v3_script door gives its script:
// whether the caller may download from area a. DOWNLOADFILE checks only the
// area's download ACS, because the caller passed its list ACS to choose the
// area; a script can name any area by its ID, so both are checked here, or
// a script could read and queue files from an area hidden from the caller.
func v3ScriptCanDownload(ctx *DoorCtx) func(*file.FileArea) bool {
	return func(a *file.FileArea) bool {
		u := ctx.CurrentUser
		return u != nil &&
			checkACS(a.ACSList, u, ctx.Session, ctx.Terminal, ctx.SessionStartTime) &&
			(a.ACSDownload == "" || checkACS(a.ACSDownload, u, ctx.Session, ctx.Terminal, ctx.SessionStartTime))
	}
}

// v3ScriptCanList is the CanList a v3_script door gives its script: whether
// the caller may see area a and its files, by the area's list ACS, as the
// file area menus check it.
func v3ScriptCanList(ctx *DoorCtx) func(*file.FileArea) bool {
	return func(a *file.FileArea) bool {
		u := ctx.CurrentUser
		return u != nil && checkACS(a.ACSList, u, ctx.Session, ctx.Terminal, ctx.SessionStartTime)
	}
}

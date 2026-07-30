package menu

// lbFrame tracks the fileLightbar's smart-refresh state across run() loop
// iterations — what was last rendered — so refreshFrame can redraw only the
// regions that changed since then.
type lbFrame struct {
	prevSelectedIndex int
	prevTopIndex      int
	prevCmdIndex      int
	prevPage          int
	needFullRedraw    bool
}

// refreshFrame redraws whichever screen regions changed since the last call:
// everything, if f.needFullRedraw is set; the whole footer and file area, if
// the viewport scrolled; or just the old/new selected rows (plus the top
// line, if the page changed) when only the selection moved within the same
// viewport. The command bar is redrawn separately whenever cmdIndex changed.
// calculatePageInfo is computed once per call and reused for both the
// page-changed check and the trailing prevPage update — it previously ran
// twice per keystroke.
func (lb *fileLightbar) refreshFrame(f *lbFrame) error {
	curPage, _ := lb.calculatePageInfo()

	if f.needFullRedraw {
		if err := lb.renderFull(); err != nil {
			return err
		}
		f.needFullRedraw = false
	} else if lb.topIndex != f.prevTopIndex {
		// Viewport scrolled — full redraw of all regions to prevent overlap.
		if err := lb.renderTop(); err != nil {
			return err
		}
		if err := lb.renderFileArea(); err != nil {
			return err
		}
		if err := lb.renderSeparator(); err != nil {
			return err
		}
		if err := lb.renderCmdBar(); err != nil {
			return err
		}
		if err := lb.renderPageIndicator(); err != nil {
			return err
		}
	} else if lb.selectedIndex != f.prevSelectedIndex {
		// Same viewport, selection changed — redraw old/new rows; redraw TOP if page changed.
		if curPage != f.prevPage {
			if err := lb.renderTop(); err != nil {
				return err
			}
		}
		if f.prevSelectedIndex >= 0 && f.prevSelectedIndex < len(lb.allFiles) {
			if row, h := lb.screenRowForFile(f.prevSelectedIndex); row >= 0 {
				if err := lb.writeFileRow(row, f.prevSelectedIndex, false, h); err != nil {
					return err
				}
			}
		}
		if row, h := lb.screenRowForFile(lb.selectedIndex); row >= 0 {
			if err := lb.writeFileRow(row, lb.selectedIndex, true, h); err != nil {
				return err
			}
		}
	}
	if lb.cmdIndex != f.prevCmdIndex {
		if err := lb.renderCmdBar(); err != nil {
			return err
		}
	}

	f.prevSelectedIndex = lb.selectedIndex
	f.prevTopIndex = lb.topIndex
	f.prevCmdIndex = lb.cmdIndex
	f.prevPage = curPage
	return nil
}

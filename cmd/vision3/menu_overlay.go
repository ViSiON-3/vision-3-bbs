package main

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// logMenuOverlay reports at startup whether the menu set's overlay directory
// (menus.d/<set>) is in use and what it contributes. The single most common
// question an overlay invites is "why isn't my change showing?", and the
// answer is usually in this line: the directory is not where the BBS looks,
// or a subdirectory name is misspelled.
func logMenuOverlay(menuSetPath string) {
	menus := menuset.FromPath(menuSetPath)
	if !menus.HasOverlay() {
		return
	}
	sum, ok, err := menus.Summarize()
	if err != nil {
		slog.Warn("menu overlay could not be read", "path", menus.Overlay, "error", err)
		return
	}
	if !ok {
		slog.Info("menu overlay not present; using the shipped menu set only",
			"path", menus.Overlay, "menus", menus.Base)
		return
	}
	slog.Info("menu overlay active", "path", menus.Overlay, "menus", menus.Base,
		"overrides", sum.Overrides, "additions", sum.Additions)
	for _, d := range sum.UnknownDirs {
		slog.Warn("menu overlay subdirectory has no counterpart in the shipped set and will never be read",
			"path", menus.Path(menuset.LayerOverlay, d))
	}
}

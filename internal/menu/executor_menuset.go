package menu

import (
	"log/slog"
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// Menus returns the menu set the executor reads from: the shipped set at
// MenuSetPath with its overlay (menus.d/<set>) searched first, file by file.
// It is derived rather than stored so a MenuExecutor built as a struct
// literal — every test does this — behaves the same as one from NewExecutor.
func (e *MenuExecutor) Menus() menuset.Set {
	return menuset.FromPath(e.MenuSetPath)
}

// menuFile resolves a file inside the menu set, overlay first. elem is the
// path relative to the set root: menuFile("ansi", "MAIN.ANS").
func (e *MenuExecutor) menuFile(elem ...string) string {
	return e.Menus().Resolve(elem...)
}

// templateFile resolves a template by its bare name, accepting the .ANS/.ans
// suffixed spellings the shipped set uses for some screens (FILEAREA.TOP.ANS).
// Each spelling is tried in the overlay before the shipped set. readTemplateFile
// probes the same suffixes itself, so passing its result on is safe either way.
func (e *MenuExecutor) templateFile(name string) string {
	return e.Menus().ResolveFirst("templates", name, name+".ANS", name+".ans")
}

// lightbarLayerWarned remembers which menus have already had their layer
// mismatch reported, so a sysop sees the warning once per process rather
// than once per menu display.
var lightbarLayerWarned sync.Map

// warnLightbarLayerMismatch logs once when a lightbar menu's .ANS, .BAR and
// .CFG do not all come from the same layer. The three files are drawn against
// each other — the BAR holds screen coordinates for the ANS, the CFG the keys
// the BAR refers to — so overriding one without the others is the most likely
// way for an overlay to produce a menu that looks right and behaves wrong.
func (e *MenuExecutor) warnLightbarLayerMismatch(menuName string) {
	m := e.Menus()
	if !m.HasOverlay() {
		return
	}
	type part struct {
		sub, name string
	}
	parts := []part{
		{"ansi", menuName + ".ANS"},
		{"bar", menuName + ".BAR"},
		{"cfg", menuName + ".CFG"},
	}
	var base, overlay []string
	for _, p := range parts {
		_, layer, ok := m.Locate(p.sub, p.name)
		if !ok {
			continue
		}
		if layer == menuset.LayerOverlay {
			overlay = append(overlay, p.name)
		} else {
			base = append(base, p.name)
		}
	}
	if len(base) == 0 || len(overlay) == 0 {
		lightbarLayerWarned.Delete(menuName) // all one layer again: warn afresh if it drifts later
		return
	}
	if _, already := lightbarLayerWarned.LoadOrStore(menuName, struct{}{}); already {
		return
	}
	slog.Warn("lightbar menu files resolve from different layers; its .ANS, .BAR and .CFG must agree",
		"menu", menuName, "overlay", overlay, "shipped", base, "overlay_dir", m.Overlay)
}

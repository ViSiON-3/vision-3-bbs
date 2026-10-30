package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// renderSysEditRow draws one field per row, so a second field on a row would be
// hidden while the arrow keys still stop on it.
func TestFieldPagesEachHaveARow(t *testing.T) {
	cfg := &config.ServerConfig{}
	pages := map[string][]fieldDef{
		"Access Levels":      sysFieldsLevels(cfg),
		"New Users & Voting": sysFieldsNUV(cfg),
		"WFC & Paging":       sysFieldsWFCPaging(cfg),
		"Toasts":             sysFieldsToasts(cfg),
	}
	for name, fields := range pages {
		t.Run(name, func(t *testing.T) {
			seen := map[int]string{}
			last := 0
			for i, f := range fields {
				if f.Row != i+1 {
					t.Errorf("%q on row %d, want %d: a gap leaves a blank line on screen", f.Label, f.Row, i+1)
				}
				if other, dup := seen[f.Row]; dup {
					t.Errorf("%q and %q share row %d", f.Label, other, f.Row)
				}
				seen[f.Row] = f.Label
				if f.Row < last {
					t.Errorf("%q on row %d comes after row %d, so the arrow keys would jump", f.Label, f.Row, last)
				}
				last = f.Row
			}
		})
	}
}

// Access Levels holds only levels; the switches beside them moved to pages of
// their own.
func TestMovedFieldsLiveOnTheirNewPages(t *testing.T) {
	cfg := &config.ServerConfig{}
	for _, label := range []string{"Auto Validate", "Notify SysOp"} {
		fieldByLabel(t, sysFieldsNUV(cfg), label)
	}
	for _, label := range []string{"WFC Access", "Page Timeout", "Page Cooldown"} {
		fieldByLabel(t, sysFieldsWFCPaging(cfg), label)
	}
	for _, f := range sysFieldsLevels(cfg) {
		if f.Type != ftInteger || f.Max != 255 {
			t.Errorf("%q is not an access level", f.Label)
		}
	}
}

func TestToastEdgeFieldDefaultsToBottom(t *testing.T) {
	cfg := &config.ServerConfig{}
	f := fieldByLabel(t, sysFieldsToasts(cfg), "Toast Edge")
	if got := f.Get(); got != config.ToastEdgeBottom {
		t.Errorf("unset edge shows %q", got)
	}
	if err := f.Set(config.ToastEdgeTop); err != nil {
		t.Fatal(err)
	}
	if cfg.ToastEdge != config.ToastEdgeTop || f.Get() != config.ToastEdgeTop {
		t.Errorf("edge = %q, shows %q", cfg.ToastEdge, f.Get())
	}
}

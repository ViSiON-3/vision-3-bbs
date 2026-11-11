package menu

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strconv"
	"testing"
)

// TestShippedMSGHDRBarLoadsWithoutWarnings checks that the header-style
// picker's standalone MSGHDR.BAR loads quietly. It has no MSGHDR.CFG, and
// loading it as a menu lightbar logged a missing-CFG warning plus one
// "no matching command" warning per style every time the picker opened.
func TestShippedMSGHDRBarLoadsWithoutWarnings(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	e := &MenuExecutor{MenuSetPath: filepath.Join("..", "..", "menus", "v3")}
	options, err := loadBarFile("MSGHDR", e)
	if err != nil {
		t.Fatalf("loadBarFile(MSGHDR): %v", err)
	}
	if len(options) == 0 {
		t.Fatal("shipped MSGHDR.BAR produced no options")
	}
	for i, opt := range options {
		if _, err := strconv.Atoi(opt.ReturnValue); err != nil {
			t.Errorf("option %d (%q): return value %q is not a template number", i, opt.Text, opt.ReturnValue)
		}
	}
	if buf.Len() > 0 {
		t.Errorf("loading MSGHDR.BAR logged warnings:\n%s", buf.String())
	}
}

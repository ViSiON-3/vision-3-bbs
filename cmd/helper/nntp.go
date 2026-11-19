package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/nntp"
	"github.com/ViSiON-3/vision-3-bbs/internal/source"
)

func cmdNNTP(args []string) {
	if len(args) == 0 || strings.EqualFold(args[0], "help") || args[0] == "--help" {
		fmt.Fprintln(os.Stderr, "Usage: helper nntp sync [--group NAME] [--dry-run] [--config DIR] [--data DIR]")
		return
	}
	if !strings.EqualFold(args[0], "sync") {
		fmt.Fprintln(os.Stderr, "Unknown NNTP subcommand:", args[0])
		os.Exit(1)
	}
	fs := flag.NewFlagSet("nntp sync", flag.ExitOnError)
	configDir := fs.String("config", "configs", "Config directory")
	dataDir := fs.String("data", "data", "Data directory")
	group := fs.String("group", "", "Sync only this enabled group")
	dry := fs.Bool("dry-run", false, "Preview without writing articles or checkpoints")
	_ = fs.Parse(args[1:])
	os.Exit(runNNTPSync(*configDir, *dataDir, *group, *dry, nntp.Dial, os.Stdout, os.Stderr))
}
func runNNTPSync(configDir, dataDir, selected string, dry bool, dial func(context.Context, nntp.Config) (*nntp.Client, error), out, errOut io.Writer) int {
	fail := func(err error) int { _, _ = fmt.Fprintln(errOut, "NNTP:", err); return 1 }
	c, err := nntp.LoadConfig(configDir)
	if err != nil {
		return fail(err)
	}
	var groups []nntp.Group
	for _, g := range c.Groups {
		if g.Enabled && (selected == "" || selected == g.Name) {
			groups = append(groups, g)
		}
	}
	if len(groups) == 0 {
		if selected != "" {
			return fail(fmt.Errorf("no enabled group %s", selected))
		}
		_, _ = fmt.Fprintln(out, "No enabled NNTP groups.")
		return 0
	}
	// One process per server/config keeps concurrent manual and scheduled runs
	// from racing their deduplication check and JAM write.
	if err := os.MkdirAll(filepath.Join(dataDir, "source"), 0o755); err != nil {
		return fail(err)
	}
	lock, err := filelock.Acquire(filepath.Join(dataDir, "source", "nntp-sync"), 0)
	if err != nil {
		return fail(fmt.Errorf("another NNTP sync may be running: %w", err))
	}
	defer lock.Release()
	server, err := config.LoadServerConfig(configDir)
	if err != nil {
		return fail(err)
	}
	mm, err := message.NewMessageManager(dataDir, configDir, server.BoardName, nil)
	if err != nil {
		return fail(err)
	}
	state, err := source.OpenStore(dataDir)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = state.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), c.MaxRun())
	defer cancel()
	client, err := dial(ctx, c)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = client.Close() }()
	importer := nntp.Importer{Config: c, Reader: client, Messages: mm, State: state, DryRun: dry, Out: out}
	status := 0
	for _, g := range groups {
		result := importer.Sync(g)
		if !dry {
			r := source.FeedResult{Source: "nntp", Feed: c.Server + "/" + g.Name, AreaTag: g.AreaTag, Items: result.Imported, Err: result.Err}
			if e := state.RecordRun(r, time.Now()); e != nil {
				_, _ = fmt.Fprintln(errOut, "NNTP run log:", e)
				status = 1
			}
		}
		if result.Err != nil {
			_, _ = fmt.Fprintf(errOut, "%s -> %s: %v\n", g.Name, g.AreaTag, result.Err)
			status = 1
			break
		}
		_, _ = fmt.Fprintf(out, "%s -> %s: %d articles, %d skipped\n", g.Name, g.AreaTag, result.Imported, result.Skipped)
	}
	return status
}

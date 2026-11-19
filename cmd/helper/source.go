package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/source"
)

// cmdSource runs the source gateway subcommands.
func cmdSource(args []string) {
	if len(args) == 0 {
		sourceUsage()
		os.Exit(1)
	}
	switch strings.ToLower(args[0]) {
	case "sync":
		fs := flag.NewFlagSet("source sync", flag.ExitOnError)
		var o sourceSyncOpts
		fs.StringVar(&o.ConfigDir, "config", "configs", "Config directory")
		fs.StringVar(&o.DataDir, "data", "data", "Data directory")
		fs.StringVar(&o.Source, "source", "", "Sync only this source's feeds")
		fs.StringVar(&o.Feed, "feed", "", "Sync only feeds with this target")
		fs.BoolVar(&o.DryRun, "dry-run", false, "Print what would be imported without writing")
		_ = fs.Parse(args[1:]) // ExitOnError
		os.Exit(runSourceSync(o, newFetcher, os.Stdout, os.Stderr))
	case "dump":
		fs := flag.NewFlagSet("source dump", flag.ExitOnError)
		name := fs.String("source", "", "Source whose definition reads the page (required)")
		url := fs.String("url", "", "Page to load (required)")
		configDir := fs.String("config", "configs", "Config directory")
		_ = fs.Parse(args[1:]) // ExitOnError
		os.Exit(runSourceDump(*configDir, *name, *url))
	case "help", "--help", "-h":
		sourceUsage()
	default:
		_, _ = fmt.Fprintf(os.Stderr, "Unknown source subcommand: %s\n\n", args[0])
		sourceUsage()
		os.Exit(1)
	}
}

func sourceUsage() {
	_, _ = fmt.Fprintf(os.Stderr, "Usage: helper source <subcommand> [options]\n\n")
	_, _ = fmt.Fprintf(os.Stderr, "  sync [--source <name>] [--feed <target>] [--dry-run]   Import new items and replies into the feeds' areas\n")
	_, _ = fmt.Fprintf(os.Stderr, "  dump --source <name> --url <url>   Print the part of a page the definition keeps (for writing definitions and fixtures)\n")
}

// cmdReddit keeps the Reddit gateway's commands: helper reddit sync is
// helper source sync --source reddit.
func cmdReddit(args []string) {
	if len(args) == 0 {
		redditUsage()
		os.Exit(1)
	}
	switch strings.ToLower(args[0]) {
	case "sync":
		os.Exit(runSourceSync(redditSyncArgs(args[1:]), newFetcher, os.Stdout, os.Stderr))
	case "dump":
		fs := flag.NewFlagSet("reddit dump", flag.ExitOnError)
		url := fs.String("url", "", "Page to load (required)")
		configDir := fs.String("config", "configs", "Config directory")
		_ = fs.Parse(args[1:]) // ExitOnError
		os.Exit(runSourceDump(*configDir, "reddit", *url))
	case "help", "--help", "-h":
		redditUsage()
	default:
		_, _ = fmt.Fprintf(os.Stderr, "Unknown reddit subcommand: %s\n\n", args[0])
		redditUsage()
		os.Exit(1)
	}
}

func redditUsage() {
	_, _ = fmt.Fprintf(os.Stderr, "Usage: helper reddit <subcommand> [options]\n\n")
	_, _ = fmt.Fprintf(os.Stderr, "  sync [--sub <name>] [--dry-run]   helper source sync --source reddit [--feed <name>]\n")
	_, _ = fmt.Fprintf(os.Stderr, "  dump --url <url>   helper source dump --source reddit --url <url>\n")
}

// redditSyncArgs reads helper reddit sync's flags as source sync options.
func redditSyncArgs(args []string) sourceSyncOpts {
	fs := flag.NewFlagSet("reddit sync", flag.ExitOnError)
	o := sourceSyncOpts{Source: "reddit"}
	fs.StringVar(&o.ConfigDir, "config", "configs", "Config directory")
	fs.StringVar(&o.DataDir, "data", "data", "Data directory")
	fs.StringVar(&o.Feed, "sub", "", "Sync only this subreddit")
	fs.BoolVar(&o.DryRun, "dry-run", false, "Print what would be imported without writing")
	_ = fs.Parse(args) // ExitOnError
	return o
}

// newFetcher opens the fetcher a definition asks for.
func newFetcher(cfg source.Config, d source.Definition) (source.Fetcher, error) {
	if d.Fetch == "http" {
		return source.NewHTTPFetcher(cfg.PageTimeout()), nil
	}
	return source.NewBrowserFetcher(cfg.ChromeDebugURL, cfg.PageTimeout())
}

// runSourceDump prints the part of one page the source's definition keeps.
func runSourceDump(configDir, name, url string) int {
	if name == "" || url == "" {
		_, _ = fmt.Fprintln(os.Stderr, "Error: --source and --url are required")
		return 1
	}
	d, err := source.LoadDefinition(configDir, name)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	cfg, err := source.LoadConfig(configDir)
	if err != nil {
		cfg = source.Config{ChromeDebugURL: "http://127.0.0.1:9222", PageTimeoutSeconds: 60}
	}
	f, err := newFetcher(cfg, d)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	defer f.Close()
	page, err := f.Page(url, d)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	_, _ = fmt.Print(page)
	return 0
}

// sleep pauses between page loads; tests replace it.
var sleep = time.Sleep

type sourceSyncOpts struct {
	ConfigDir, DataDir, Source, Feed string
	DryRun                           bool
}

// feedLabel names a feed in output lines: r/<sub> for Reddit, as the Reddit
// gateway printed, and <source>/<target> for everything else.
func feedLabel(f source.Feed) string {
	if f.Source == "reddit" {
		return "r/" + f.Target
	}
	return f.Source + "/" + f.Target
}

func resultLine(r source.FeedResult) string {
	if r.Source == "reddit" {
		return fmt.Sprintf("%d posts, %d comments, %d pages", r.Items, r.Replies, r.Pages)
	}
	return fmt.Sprintf("%d items, %d replies, %d pages", r.Items, r.Replies, r.Pages)
}

// runSourceSync syncs every enabled feed, or those o selects, and returns
// the exit status: 0 when every feed synced, 1 otherwise.
// Summary lines go to out and failures to errOut: the event scheduler logs
// a failed run's stderr, so that is where the reason has to be.
func runSourceSync(o sourceSyncOpts, open func(source.Config, source.Definition) (source.Fetcher, error), out, errOut io.Writer) int {
	fail := func(format string, a ...any) int {
		_, _ = fmt.Fprintf(errOut, "Error: "+format+"\n", a...)
		return 1
	}
	cfg, err := source.LoadConfig(o.ConfigDir)
	if err != nil {
		return fail("%v", err)
	}
	if editedSelectors(filepath.Join(o.ConfigDir, "reddit_selectors.json")) {
		_, _ = fmt.Fprintln(out, "Note: reddit_selectors.json is no longer read; the Reddit definition is configs/sources/reddit.json.")
	}
	var feeds []source.Feed
	for _, f := range cfg.Feeds {
		if o.Source != "" && !strings.EqualFold(f.Source, o.Source) {
			continue
		}
		// A feed named on the command line runs even when disabled.
		if (o.Feed == "" && f.Enabled) || (o.Feed != "" && strings.EqualFold(f.Target, o.Feed)) {
			feeds = append(feeds, f)
		}
	}
	if len(feeds) == 0 {
		switch {
		case o.Feed != "":
			return fail("no %s feed %s in the config", or(o.Source, "source"), o.Feed)
		case o.Source != "":
			if _, err := source.LoadDefinition(o.ConfigDir, o.Source); err != nil {
				return fail("%v", err)
			}
			_, _ = fmt.Fprintf(out, "No enabled %s feeds.\n", o.Source)
			return 0
		}
		_, _ = fmt.Fprintln(out, "No enabled feeds.")
		return 0
	}
	serverCfg, err := config.LoadServerConfig(o.ConfigDir)
	if err != nil {
		return fail("loading config.json: %v", err)
	}
	mm, err := message.NewMessageManager(o.DataDir, o.ConfigDir, serverCfg.BoardName, nil)
	if err != nil {
		return fail("opening message areas: %v", err)
	}
	store, err := source.OpenStore(o.DataDir)
	if err != nil {
		return fail("%v", err)
	}
	defer func() { _ = store.Close() }() // read-mostly; writes are checked where they happen

	deadline := time.Now().Add(cfg.MaxRun())
	fetchers := map[string]source.Fetcher{}
	importers := map[string]*source.Importer{}
	openErrs := map[string]error{}
	defer func() {
		for _, f := range fetchers {
			f.Close()
		}
	}()
	record := func(r source.FeedResult, f source.Feed) {
		if o.DryRun {
			return
		}
		if err := store.RecordRun(r, time.Now()); err != nil {
			_, _ = fmt.Fprintf(errOut, "%s: could not log the run: %v\n", feedLabel(f), err)
		}
	}
	status := 0
	for i, f := range feeds {
		d, err := source.LoadDefinition(o.ConfigDir, f.Source)
		if err != nil {
			status = 1
			_, _ = fmt.Fprintf(errOut, "%s -> %s: error: %v\n", feedLabel(f), f.AreaTag, err)
			continue
		}
		// One importer per source, so its feeds share the pause between page
		// loads: the site sees one paced run, not one per feed.
		if err, failed := openErrs[d.Name]; failed {
			status = 1
			_, _ = fmt.Fprintf(errOut, "%s -> %s: error: %v\n", feedLabel(f), f.AreaTag, err)
			continue
		}
		im, ok := importers[d.Name]
		if !ok {
			fetcher, err := open(cfg, d)
			if err != nil {
				// This source's other feeds fail the same way; other sources
				// still run.
				openErrs[d.Name] = err
				status = 1
				_, _ = fmt.Fprintf(errOut, "%s -> %s: error: %v\n", feedLabel(f), f.AreaTag, err)
				continue
			}
			fetchers[d.Name] = fetcher
			im = &source.Importer{MM: mm, Fetcher: fetcher, Def: d, Store: store,
				PageDelay: d.PageDelay(cfg.PageDelay()), PageTimeout: cfg.PageTimeout(), MaxItems: cfg.MaxItemsPerSync,
				Sleep: sleep, Deadline: deadline, DryRun: o.DryRun, Out: out}
			importers[d.Name] = im
		}
		r := im.SyncFeed(f)
		record(r, f)
		switch {
		case errors.Is(r.Err, source.ErrRunTimeLimit):
			// Not a failure: the next run carries on from here.
			_, _ = fmt.Fprintf(out, "%s -> %s: %s; %v\n", feedLabel(f), f.AreaTag, resultLine(r), r.Err)
			for _, rest := range feeds[i+1:] {
				_, _ = fmt.Fprintf(out, "%s: not reached this run\n", feedLabel(rest))
			}
			return status
		case r.Err != nil:
			status = 1
			_, _ = fmt.Fprintf(errOut, "%s -> %s: error: %v\n", feedLabel(f), f.AreaTag, r.Err)
		default:
			_, _ = fmt.Fprintf(out, "%s -> %s: %s\n", feedLabel(f), f.AreaTag, resultLine(r))
		}
	}
	return status
}

// shippedSelectors are the SHA-256 sums of every reddit_selectors.json the
// setup scripts copied onto boards. Only an edited copy is worth a note.
var shippedSelectors = map[string]bool{
	"5937594a019f38feb102b0b9c43897fa443f195a00a8454dd5b6c254f1c1511e": true,
	"8e2845462639003001c3914425ad58259a30a321b0b19fefa713820de7787861": true,
}

// editedSelectors reports whether path holds a reddit_selectors.json the
// sysop changed, whose changes now belong in configs/sources/reddit.json.
func editedSelectors(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	// A Windows checkout gives the shipped copy CRLF line endings; the sums
	// are of the LF form.
	sum := sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
	return !shippedSelectors[hex.EncodeToString(sum[:])]
}

func or(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}

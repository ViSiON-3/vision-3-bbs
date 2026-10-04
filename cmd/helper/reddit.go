package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/reddit"
)

// cmdReddit runs the Reddit gateway subcommands.
func cmdReddit(args []string) {
	if len(args) == 0 {
		redditUsage()
		os.Exit(1)
	}
	switch strings.ToLower(args[0]) {
	case "sync":
		os.Exit(cmdRedditSync(args[1:]))
	case "dump":
		os.Exit(cmdRedditDump(args[1:]))
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
	_, _ = fmt.Fprintf(os.Stderr, "  sync [--sub <name>] [--dry-run]   Import new posts and comments into the mapped areas\n")
	_, _ = fmt.Fprintf(os.Stderr, "  dump --url <url>   Print a page's content section (for updating selectors and test fixtures)\n")
}

// cmdRedditDump prints one page's content section to stdout.
func cmdRedditDump(args []string) int {
	fs := flag.NewFlagSet("reddit dump", flag.ExitOnError)
	url := fs.String("url", "", "Page to load (required)")
	configDir := fs.String("config", "configs", "Config directory")
	_ = fs.Parse(args) // ExitOnError

	if *url == "" {
		_, _ = fmt.Fprintln(os.Stderr, "Error: --url is required")
		return 1
	}
	sel, err := reddit.LoadSelectors(*configDir)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	debugURL := "http://127.0.0.1:9222"
	if cfg, err := reddit.LoadConfig(*configDir); err == nil {
		debugURL = cfg.ChromeDebugURL
	}
	f, err := reddit.NewChromeFetcher(debugURL, sel, 60*time.Second)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	defer f.Close()
	page, err := f.Fetch(*url)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	_, _ = fmt.Print(page)
	return 0
}

type redditSyncOpts struct {
	ConfigDir, DataDir, Sub string
	DryRun                  bool
}

func cmdRedditSync(args []string) int {
	fs := flag.NewFlagSet("reddit sync", flag.ExitOnError)
	var o redditSyncOpts
	fs.StringVar(&o.ConfigDir, "config", "configs", "Config directory")
	fs.StringVar(&o.DataDir, "data", "data", "Data directory")
	fs.StringVar(&o.Sub, "sub", "", "Sync only this subreddit")
	fs.BoolVar(&o.DryRun, "dry-run", false, "Print what would be imported without writing")
	_ = fs.Parse(args) // ExitOnError
	return runRedditSync(o, func(debugURL string, sel reddit.Selectors) (reddit.Fetcher, error) {
		return reddit.NewChromeFetcher(debugURL, sel, 60*time.Second)
	}, os.Stdout)
}

// runRedditSync syncs every enabled subreddit (or just o.Sub) and returns the
// exit status: 0 when every subreddit synced, 1 otherwise.
func runRedditSync(o redditSyncOpts, newFetcher func(debugURL string, sel reddit.Selectors) (reddit.Fetcher, error), out io.Writer) int {
	fail := func(format string, a ...any) int {
		_, _ = fmt.Fprintf(out, "Error: "+format+"\n", a...)
		return 1
	}
	cfg, err := reddit.LoadConfig(o.ConfigDir)
	if err != nil {
		return fail("%v", err)
	}
	sel, err := reddit.LoadSelectors(o.ConfigDir)
	if err != nil {
		return fail("%v", err)
	}
	var subs []reddit.Mapping
	for _, m := range cfg.Subreddits {
		if (o.Sub == "" && m.Enabled) || strings.EqualFold(m.Subreddit, o.Sub) {
			subs = append(subs, m)
		}
	}
	if len(subs) == 0 {
		if o.Sub != "" {
			return fail("subreddit %s is not in reddit.json", o.Sub)
		}
		_, _ = fmt.Fprintln(out, "No enabled subreddits in reddit.json.")
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
	store, err := reddit.OpenStore(filepath.Join(o.DataDir, "reddit", "reddit.db"))
	if err != nil {
		return fail("%v", err)
	}
	defer func() { _ = store.Close() }() // read-mostly; writes are checked where they happen
	f, err := newFetcher(cfg.ChromeDebugURL, sel)
	if err != nil {
		return fail("%v", err)
	}
	defer f.Close()

	im := &reddit.Importer{MM: mm, Fetcher: f, Sel: sel, Cfg: cfg, Store: store, Sleep: time.Sleep, DryRun: o.DryRun, Out: out}
	status := 0
	for _, m := range subs {
		r := im.SyncSubreddit(m)
		if !o.DryRun {
			if err := store.RecordRun(r, time.Now()); err != nil {
				_, _ = fmt.Fprintf(out, "r/%s: could not log the run: %v\n", r.Subreddit, err)
			}
		}
		if r.Err != nil {
			status = 1
			_, _ = fmt.Fprintf(out, "r/%s -> %s: error: %v\n", r.Subreddit, r.AreaTag, r.Err)
			continue
		}
		_, _ = fmt.Fprintf(out, "r/%s -> %s: %d posts, %d comments, %d pages\n", r.Subreddit, r.AreaTag, r.Posts, r.Comments, r.Pages)
	}
	return status
}

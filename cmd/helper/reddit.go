package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/reddit"
)

// cmdReddit runs the Reddit gateway subcommands.
func cmdReddit(args []string) {
	if len(args) == 0 {
		redditUsage()
		os.Exit(1)
	}
	switch strings.ToLower(args[0]) {
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

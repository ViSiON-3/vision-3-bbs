package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// cmdPoll implements 'v3mail poll': fetch and send mail for every network in
// one go. For FTN it packs outbound mail, calls each hub with binkd and tosses
// what arrived; for QWK it runs the same exchange as qwk-poll. It is what a
// sysop reaches for when a network seems quiet and waiting for the next
// scheduled poll is not an answer.
func cmdPoll(args []string) {
	fs := flag.NewFlagSet("poll", flag.ExitOnError)
	configDir := fs.String("config", "configs", "Config directory")
	dataDir := fs.String("data", "data", "Data directory")
	networkName := fs.String("network", "", "Only this network (FTN or QWK key; default: every enabled network)")
	ftnOnly := fs.Bool("ftn-only", false, "Poll FTN networks only")
	qwkOnly := fs.Bool("qwk-only", false, "Poll QWK networks only")
	timeout := fs.Duration("timeout", 5*time.Minute, "Longest a single binkd call may take")
	verbose := fs.Bool("v", false, "Show binkd's session output")
	_ = fs.Parse(args) // ExitOnError: Parse exits the program on failure

	if *ftnOnly && *qwkOnly {
		fatalf("poll: --ftn-only and --qwk-only cannot be used together")
	}
	doFTN, doQWK := !*qwkOnly, !*ftnOnly
	ftnKey, qwkKey := "", ""
	if *networkName != "" {
		var err error
		ftnKey, qwkKey, err = resolvePollNetwork(*configDir, *networkName)
		if err != nil {
			fatalf("poll: %v", err)
		}
		doFTN = doFTN && ftnKey != ""
		doQWK = doQWK && qwkKey != ""
		if !doFTN && !doQWK {
			fatalf("poll: network %q is not the kind selected by --ftn-only/--qwk-only", *networkName)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	failed := false
	if doFTN && pollFTN(ctx, *configDir, *dataDir, ftnKey, *timeout, *verbose) {
		failed = true
	}
	if doQWK && !hasEnabledQWK(*configDir, qwkKey) {
		fmt.Println("QWK: no enabled networks")
		doQWK = false
	}
	if doQWK {
		nodes, cleanup, err := loadQWKNetNodes(*configDir, *dataDir, qwkKey)
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "QWK: %v\n", err)
			failed = true
		case len(nodes) == 0:
			fmt.Println("QWK: no enabled networks")
			cleanup()
		default:
			fmt.Println("== QWK ==")
			if pollQWK(ctx, nodes) {
				failed = true
			}
			cleanup()
		}
	}
	if failed {
		os.Exit(1)
	}
}

// resolvePollNetwork finds which kinds of network name refers to, returning
// the key as spelled in ftn.json and in qwknet.json (empty where it is not
// one). The same name may be both.
func resolvePollNetwork(configDir, name string) (ftnKey, qwkKey string, err error) {
	if ftnCfg, ferr := config.LoadFTNConfig(configDir); ferr == nil {
		ftnKey = matchKey(ftnCfg.Networks, name)
	}
	if qcfg, qerr := config.LoadQWKNetConfig(configDir); qerr == nil {
		qwkKey = matchKey(qcfg.Networks, name)
	}
	if ftnKey == "" && qwkKey == "" {
		return "", "", fmt.Errorf("network %q is in neither ftn.json nor qwknet.json", name)
	}
	return ftnKey, qwkKey, nil
}

// hasEnabledQWK reports whether there is a QWK network to poll: the one keyed
// only, or any enabled one. Checked before loadQWKNetNodes, which creates the
// QWK dupe database, so an FTN-only board is left untouched.
func hasEnabledQWK(configDir, only string) bool {
	qcfg, err := config.LoadQWKNetConfig(configDir)
	if err != nil {
		return true // let loadQWKNetNodes report the problem
	}
	if only != "" {
		return true // resolvePollNetwork already found it
	}
	for _, nc := range qcfg.Networks {
		if nc.Enabled {
			return true
		}
	}
	return false
}

// matchKey returns the key of m equal to name ignoring case, or "".
func matchKey[V any](m map[string]V, name string) string {
	for k := range m {
		if strings.EqualFold(k, name) {
			return k
		}
	}
	return ""
}

// ftnPollTarget is one hub binkd is asked to call.
type ftnPollTarget struct {
	Network string // ftn.json key, which is also the binkd domain
	Address string // hub address, without the @domain
}

// ftnPollPlan lists the hubs to call for every enabled network, or only the
// network keyed only. Links without a hostname cannot be called — the hub has
// to call in — so they are returned separately for the sysop to see.
func ftnPollPlan(cfg config.FTNConfig, only string) (targets, uncallable []ftnPollTarget) {
	names := make([]string, 0, len(cfg.Networks))
	for name, nc := range cfg.Networks {
		if !nc.InternalTosserEnabled {
			continue
		}
		if only != "" && name != only {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, lnk := range cfg.Networks[name].Links {
			t := ftnPollTarget{Network: name, Address: lnk.Address}
			if lnk.HostPort() == "" {
				uncallable = append(uncallable, t)
			} else {
				targets = append(targets, t)
			}
		}
	}
	return targets, uncallable
}

// pollFTN packs outbound mail, calls each hub and tosses whatever arrived,
// reporting whether anything failed.
func pollFTN(ctx context.Context, configDir, dataDir, only string, timeout time.Duration, verbose bool) bool {
	// Checked before loadFTNDeps, which creates the FTN dupe database, so a
	// QWK-only board is left untouched.
	if raw, err := config.LoadFTNConfig(configDir); err == nil {
		if t, u := ftnPollPlan(raw, only); len(t) == 0 && len(u) == 0 {
			fmt.Println("FTN: no enabled networks")
			return false
		}
	}
	ftnCfg, msgMgr, dupeDB, err := loadFTNDeps(configDir, dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FTN: %v\n", err)
		return true
	}
	defer func() { _ = msgMgr.Close() }()

	targets, uncallable := ftnPollPlan(ftnCfg, only)
	fmt.Println("== FTN ==")

	// Pack first so this call also delivers anything waiting to go out.
	failed := scanFTN(ftnCfg, msgMgr, dupeDB, only, false)
	if packFTN(ftnCfg, msgMgr, dupeDB, only, false) {
		failed = true
	}

	for _, t := range uncallable {
		fmt.Printf("[%s] %s has no hostname — it can only be reached when it calls us\n", t.Network, t.Address)
	}
	if len(targets) > 0 {
		root := bbsRootFromData(dataDir)
		binkd := ftnCfg.Binkd.BinaryPath
		if !filepath.IsAbs(binkd) {
			binkd = filepath.Join(root, binkd)
		}
		conf := filepath.Join(root, "data", "ftn", "binkd.conf")
		if _, err := os.Stat(conf); err != nil {
			fmt.Fprintf(os.Stderr, "FTN: %s not found — start the BBS once or run the FTN Setup Wizard to create it\n", conf)
			return true
		}
		for _, t := range targets {
			if ctx.Err() != nil {
				return true
			}
			if !callHub(ctx, binkd, conf, root, t, timeout, verbose) {
				failed = true
			}
		}
	}

	// Every enabled network tosses, not just the one polled: networks can
	// share an inbound, and a packet is only claimed by its own network.
	if tossFTN(ftnCfg, msgMgr, dupeDB, "", false) {
		failed = true
	}
	return failed
}

// binkdTransfer matches binkd's per-file log lines, e.g.
// "+ 27 Sep 10:00:01 [123] rcvd: 0df9b0c6.pkt (1644, 1644.00 CPS, 21:1/100@fsxnet)".
var binkdTransfer = regexp.MustCompile(`\b(rcvd|sent): `)

// callHub runs one binkd poll of t and prints a one-line summary.
func callHub(ctx context.Context, binkd, conf, root string, t ftnPollTarget, timeout time.Duration, verbose bool) bool {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fmt.Printf("[%s] calling %s...\n", t.Network, t.Address)
	cmd := exec.CommandContext(cctx, binkd, "-p", "-P", t.Address+"@"+t.Network, conf)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()

	if verbose {
		os.Stdout.Write(out.Bytes())
	}
	sent, rcvd := countTransfers(out.String())
	switch {
	case errors.Is(cctx.Err(), context.DeadlineExceeded):
		fmt.Fprintf(os.Stderr, "[%s] %s: gave up after %s\n", t.Network, t.Address, timeout)
	case err != nil:
		fmt.Fprintf(os.Stderr, "[%s] %s: binkd failed: %v\n", t.Network, t.Address, err)
	default:
		fmt.Printf("[%s] %s: sent %d, received %d file(s)\n", t.Network, t.Address, sent, rcvd)
		return true
	}
	if !verbose {
		for _, line := range lastLines(out.String(), 8) {
			fmt.Fprintf(os.Stderr, "    %s\n", line)
		}
	}
	fmt.Fprintf(os.Stderr, "    full session log: data/logs/binkd.log\n")
	return false
}

// countTransfers counts the files binkd reports sending and receiving.
func countTransfers(output string) (sent, rcvd int) {
	for _, m := range binkdTransfer.FindAllStringSubmatch(output, -1) {
		if m[1] == "sent" {
			sent++
		} else {
			rcvd++
		}
	}
	return sent, rcvd
}

// lastLines returns up to n trailing non-empty lines of s.
func lastLines(s string, n int) []string {
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, strings.TrimRight(l, "\r"))
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// bbsRootFromData is the BBS root as loadFTNDeps derives it: the parent of the
// data directory.
func bbsRootFromData(dataDir string) string {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = dataDir
	}
	return filepath.Dir(abs)
}

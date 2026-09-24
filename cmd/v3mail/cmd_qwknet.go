package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwknet"
)

// qwkNetFlags parses the flags shared by the qwk-* commands.
func qwkNetFlags(name string, args []string) (configDir, dataDir, network string) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.StringVar(&configDir, "config", "configs", "Config directory")
	fs.StringVar(&dataDir, "data", "data", "Data directory")
	fs.StringVar(&network, "network", "", "Only this QWK network key (default: every enabled network)")
	_ = fs.Parse(args)
	return configDir, dataDir, network
}

// loadQWKNetNodes builds a node for each enabled network (or the named one).
func loadQWKNetNodes(configDir, dataDir, only string) ([]*qwknet.Node, func(), error) {
	qcfg, err := config.LoadQWKNetConfig(configDir)
	if err != nil {
		return nil, nil, err
	}
	// Paths in qwknet.json are relative to the BBS root, which is the parent
	// of the configs directory in every supported layout.
	root, err := filepath.Abs(filepath.Join(configDir, ".."))
	if err != nil {
		return nil, nil, err
	}
	qcfg.ResolvePaths(root)

	serverCfg, err := config.LoadServerConfig(configDir)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config.json: %w", err)
	}
	systemID := config.NormalizeQWKID(serverCfg.QWKID)
	if systemID == "" {
		systemID = config.NormalizeQWKID(serverCfg.BoardName)
	}

	msgMgr, err := message.NewMessageManager(dataDir, configDir, serverCfg.BoardName, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("opening message areas: %w", err)
	}
	dupes, err := qwknet.OpenDupeDB(qcfg.DupeDBPath)
	if err != nil {
		return nil, nil, fmt.Errorf("opening dupe database: %w", err)
	}

	var nodes []*qwknet.Node
	for _, key := range qcfg.NetworkKeys() {
		nc := qcfg.Networks[key]
		if only != "" && !strings.EqualFold(key, only) {
			continue
		}
		if !nc.Enabled && only == "" {
			continue
		}
		n, err := qwknet.New(key, nc, qcfg, systemID, msgMgr, dupes)
		if err != nil {
			return nil, nil, err
		}
		nodes = append(nodes, n)
	}
	if only != "" && len(nodes) == 0 {
		return nil, nil, fmt.Errorf("QWK network %q is not configured in %s", only, filepath.Join(configDir, "qwknet.json"))
	}
	cleanup := func() { _ = dupes.Save() }
	return nodes, cleanup, nil
}

// cmdQWKPoll runs the full exchange with each hub: toss, scan, upload,
// download, toss.
func cmdQWKPoll(args []string) {
	configDir, dataDir, only := qwkNetFlags("qwk-poll", args)
	nodes, cleanup, err := loadQWKNetNodes(configDir, dataDir, only)
	if err != nil {
		fatalf("qwk-poll: %v", err)
	}
	defer cleanup()
	if len(nodes) == 0 {
		fmt.Println("No enabled QWK networks in qwknet.json; nothing to poll.")
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	failed := false
	for _, n := range nodes {
		fmt.Printf("Polling %s (hub %s as %s)...\n", n.Key, n.HubID(), n.NodeID())
		res := n.Poll(ctx)
		fmt.Printf("  packed %d new (%d pending), uploaded=%v, downloaded=%v (%d bytes), tossed %d, dupes %d, unmapped %d\n",
			res.Scan.Exported, res.Scan.Pending, res.Uploaded, res.Downloaded, res.Bytes,
			res.Toss.Imported, res.Toss.Duplicates, res.Toss.Unmapped)
		for _, e := range append(append(res.Errors, res.Scan.Errors...), res.Toss.Errors...) {
			fmt.Printf("  ERROR: %s\n", e)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

// cmdQWKScan packs new local posts into each hub's REP without connecting.
func cmdQWKScan(args []string) {
	configDir, dataDir, only := qwkNetFlags("qwk-scan", args)
	nodes, cleanup, err := loadQWKNetNodes(configDir, dataDir, only)
	if err != nil {
		fatalf("qwk-scan: %v", err)
	}
	defer cleanup()
	failed := false
	for _, n := range nodes {
		res := n.Scan()
		fmt.Printf("%s: packed %d new message(s), %d pending", n.Key, res.Exported, res.Pending)
		if res.REPPath != "" {
			fmt.Printf(" -> %s", res.REPPath)
		}
		fmt.Println()
		for _, e := range res.Errors {
			fmt.Printf("  ERROR: %s\n", e)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

// cmdQWKToss imports the packets waiting in the inbound directory.
func cmdQWKToss(args []string) {
	configDir, dataDir, only := qwkNetFlags("qwk-toss", args)
	nodes, cleanup, err := loadQWKNetNodes(configDir, dataDir, only)
	if err != nil {
		fatalf("qwk-toss: %v", err)
	}
	defer cleanup()
	failed := false
	for _, n := range nodes {
		res := n.Toss()
		fmt.Printf("%s: %d packet(s), imported %d, dupes %d, unmapped %d, skipped %d\n",
			n.Key, res.Packets, res.Imported, res.Duplicates, res.Unmapped, res.Skipped)
		for _, e := range res.Errors {
			fmt.Printf("  ERROR: %s\n", e)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

// cmdQWKConferences lists a hub's conference numbers and names, which is
// what a message area needs in its QWK Conference field.
func cmdQWKConferences(args []string) {
	configDir, dataDir, only := qwkNetFlags("qwk-conferences", args)
	if only == "" {
		fatalf("qwk-conferences: --network is required")
	}
	nodes, cleanup, err := loadQWKNetNodes(configDir, dataDir, only)
	if err != nil {
		fatalf("qwk-conferences: %v", err)
	}
	defer cleanup()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for _, n := range nodes {
		confs, err := n.FetchConferences(ctx)
		if err != nil {
			fatalf("qwk-conferences: %v", err)
		}
		fmt.Printf("Conferences on hub %s (%s):\n", n.HubID(), n.Key)
		for _, c := range confs {
			fmt.Printf("  %6d  %s\n", c.Number, c.Name)
		}
	}
}

func fatalf(format string, a ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}

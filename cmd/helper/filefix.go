package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
)

// cmdFilefix implements 'helper filefix': send a FileFix netmail to a
// network hub to change its file echo subscriptions. The counterpart of
// areafix for file echoes; hubs take the same +TAG / -TAG / %LIST commands,
// addressed to a robot usually named FileFix, AllFix or Filemgr.
func cmdFilefix(args []string) {
	fs := flag.NewFlagSet("filefix", flag.ExitOnError)
	network := fs.String("network", "", "FTN network name (required)")
	command := fs.String("command", "", "FileFix command, e.g. %LIST or +NODELIST (required unless --seed)")
	seed := fs.Bool("seed", false, "Subscribe to every file echo that has a file area in file_areas.json for this network")
	linkAddr := fs.String("link", "", "Hub address (default: first link of network)")
	configDir := fs.String("config", "configs", "Config directory")

	fs.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: helper filefix [options]\n\n")
		_, _ = fmt.Fprintf(os.Stderr, "Send a FileFix netmail message to a network hub to change file echo subscriptions.\n")
		_, _ = fmt.Fprintf(os.Stderr, "To: <filefix_name>, Subject: <filefix_password>, Body: <command>---\n")
		_, _ = fmt.Fprintf(os.Stderr, "The robot name defaults to %s; the password to the link's tic_password.\n\n", config.DefaultFilefixName)
		_, _ = fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(os.Stderr, "\nFileFix commands: %%HELP %%LIST %%QUERY %%UNLINKED +echo -echo\n")
		_, _ = fmt.Fprintf(os.Stderr, "\nExamples:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  helper filefix --network fsxnet --command \"%%LIST\"\n")
		_, _ = fmt.Fprintf(os.Stderr, "  helper filefix --network fsxnet --seed\n")
	}
	_ = fs.Parse(args) // ExitOnError: Parse exits the program on failure

	if *network == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Error: --network is required\n\n")
		fs.Usage()
		os.Exit(1)
	}
	if !*seed && *command == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Error: --command or --seed is required\n\n")
		fs.Usage()
		os.Exit(1)
	}

	hub := mustResolveHub(*configDir, *network, *linkAddr)
	password := hub.link.FilefixPass()
	if password == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Error: link %s has neither a filefix_password nor a tic_password configured\n", hub.link.Address)
		os.Exit(1)
	}
	robot := hub.link.FilefixRobot()

	var lines []string
	if *seed {
		areas, err := loadFileAreas(*configDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		lines = filefixSeedLines(areas, hub.netKey)
		if len(lines) == 0 {
			_, _ = fmt.Fprintf(os.Stderr, "Error: no file echo areas found for network %s in file_areas.json — create them first (helper fileecho)\n", hub.netKey)
			os.Exit(1)
		}
		fmt.Printf("Subscribing to %d file echoes for network %s\n", len(lines), hub.netKey)
	} else {
		lines = []string{*command}
	}

	pktPath, err := writeRobotNetmail(*configDir, hub, robot, password, robotBody(lines), "filefix_*.pkt")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("FileFix netmail written to %s\n", pktPath)
	fmt.Printf("  To: %s @ %s\n", robot, hub.link.Address)
	if *seed {
		fmt.Printf("  Subscribed to %d file echoes\n", len(lines))
	} else {
		fmt.Printf("  Command: %s\n", *command)
	}
}

// filefixSeedLines returns a "+TAG" FileFix command for each file echo of
// network that feeds a file area, once per echo, in file_areas.json order.
func filefixSeedLines(areas []file.FileArea, network string) []string {
	var lines []string
	seen := make(map[string]bool)
	for _, a := range areas {
		if !a.IsFileEcho() || !strings.EqualFold(a.Network, network) {
			continue
		}
		tag := strings.TrimSpace(a.FileEcho)
		key := strings.ToUpper(tag)
		if tag == "" || seen[key] {
			continue
		}
		seen[key] = true
		lines = append(lines, "+"+tag)
	}
	return lines
}

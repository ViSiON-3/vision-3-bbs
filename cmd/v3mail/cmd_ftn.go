package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/tosser"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// cmdToss implements 'v3mail toss': unpack FTN bundles and toss .PKT files into JAM bases.
func cmdToss(args []string) {
	fs := flag.NewFlagSet("toss", flag.ExitOnError)
	configDir := fs.String("config", "configs", "Config directory")
	dataDir := fs.String("data", "data", "Data directory")
	networkName := fs.String("network", "", "Limit to a single network (default: all enabled)")
	quiet := fs.Bool("q", false, "Quiet mode")
	_ = fs.Parse(args) // ExitOnError: Parse exits the program on failure

	ftnCfg, msgMgr, dupeDB, err := loadFTNDeps(*configDir, *dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if tossFTN(ftnCfg, msgMgr, dupeDB, loadRecipients(*dataDir), loadFileAreas(*dataDir, *configDir), *networkName, *quiet) {
		os.Exit(1)
	}
}

// tossFTN tosses inbound mail for every enabled network, or only networkName
// when set, and reports whether the run failed: a toss error, or mail left
// unclaimed long enough to be quarantined. Netmail for this system is
// addressed to the handle recipients resolves its To to; recipients may be nil
// (see tosser.Tosser.SetRecipientResolver). Inbound file echoes are delivered
// into fileAreas; nil leaves TICs in the inbound (see tosser.Tosser.SetFileAreas).
func tossFTN(ftnCfg config.FTNConfig, msgMgr *message.MessageManager, dupeDB *tosser.DupeDB, recipients user.RecipientResolver, fileAreas tosser.FileAreaStore, networkName string, quiet bool) bool {
	tosser.WarnOrphanFTNAreas(ftnCfg, msgMgr.ListAreas())

	totalImported, totalDupes, totalPackets := 0, 0, 0
	totalFiles, totalFileDupes, totalFilesBad := 0, 0, 0
	// TICs some network claimed and is holding until its file arrives. Any
	// other network that declined them must not get them reported unclaimed.
	waitingTICs := map[string]bool{}
	hadErrors := false
	// Merged across networks and keyed by inbound file, so the whole-pass
	// check can discard the origins of anything a later network claimed.
	skippedByFile := map[string]map[string]int{}
	// The whole-pass check is only sound once every enabled network has
	// actually tossed: a tosser that failed to construct might have been the
	// one to claim the mail, so its absence has to suppress the check as
	// surely as --network does.
	ranAllNetworks := networkName == ""

	for name, netCfg := range ftnCfg.Networks {
		if !netCfg.InternalTosserEnabled {
			continue
		}
		if networkName != "" && name != networkName {
			continue
		}

		t, err := tosser.New(name, netCfg, ftnCfg, dupeDB, msgMgr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating tosser for %s: %v\n", name, err)
			hadErrors = true
			ranAllNetworks = false
			continue
		}
		t.SetRecipientResolver(recipients)
		if fileAreas != nil {
			t.SetFileAreas(fileAreas)
		}

		result := t.ProcessInbound()
		totalPackets += result.PacketsProcessed
		totalImported += result.MessagesImported
		totalDupes += result.DupesSkipped
		totalFiles += result.FilesImported
		totalFileDupes += result.FilesDuped
		totalFilesBad += result.FilesBad
		for _, p := range result.WaitingTICs {
			waitingTICs[p] = true
		}
		// Replace rather than sum: every enabled network passes over the same
		// packets in a shared inbound directory and reports the same origins
		// for the same file.
		for file, origins := range result.SkippedByFile {
			if _, seen := skippedByFile[file]; !seen {
				skippedByFile[file] = origins
			}
		}

		if !quiet {
			fmt.Printf("[%s] toss: %d packets, %d imported, %d dupes",
				name, result.PacketsProcessed, result.MessagesImported, result.DupesSkipped)
			if n := result.FilesImported + result.FilesDuped + result.FilesBad; n > 0 {
				fmt.Printf("; file echoes: %d received, %d dupes, %d bad",
					result.FilesImported, result.FilesDuped, result.FilesBad)
				if result.FilesRemoved > 0 {
					fmt.Printf(", %d replaced files removed", result.FilesRemoved)
				}
			}
			if len(result.Errors) > 0 {
				fmt.Printf(", %d errors", len(result.Errors))
			}
			fmt.Println()
		}
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "  [%s] ERROR: %s\n", name, e)
			hadErrors = true
		}
	}

	// Only meaningful once every enabled network has had its turn: a bundle is
	// removed by whichever tosser takes it, so what remains is mail nobody
	// would take. Limiting the run to one network with --network says nothing
	// about what the others would have claimed.
	var unclaimed tosser.UnclaimedReport
	if ranAllNetworks {
		for p := range waitingTICs {
			delete(skippedByFile, p)
		}
		unclaimed = tosser.FindUnclaimed(ftnCfg, skippedByFile)
		unclaimed.QuarantineStale(ftnCfg.TempPath)
		unclaimed.Log()
	}

	if !quiet {
		fmt.Printf("Toss complete: %d packets, %d messages imported, %d dupes skipped\n",
			totalPackets, totalImported, totalDupes)
		if n := totalFiles + totalFileDupes + totalFilesBad; n > 0 {
			fmt.Printf("File echoes: %d files received, %d dupes dropped, %d undeliverable",
				totalFiles, totalFileDupes, totalFilesBad)
			if totalFilesBad > 0 {
				fmt.Printf(" (moved to %s)", filepath.Join(ftnCfg.TempPath, tosser.BadTICDirName))
			}
			fmt.Println()
		}
		if n := len(unclaimed.Held); n > 0 {
			// Stated plainly rather than as a warning: this mail waits because
			// the sysop switched the network off, and it tosses normally once
			// the network is switched back on.
			fmt.Printf("%d inbound file(s) held for a network whose tosser is off — enable it to toss them\n", n)
		}
		if n := len(unclaimed.Files) + len(unclaimed.Quarantined); n > 0 {
			// Without this the run looks identical to one that had no mail
			// waiting, which is how a backlog goes unnoticed for weeks.
			fmt.Printf("WARNING: %d inbound file(s) matched no configured network", n)
			if origins := unclaimed.OriginList(); origins != "" {
				fmt.Printf(" — from %s", origins)
			}
			fmt.Println()
			fmt.Println("         Check that each network's links list the address its mail comes from.")
			if len(unclaimed.Quarantined) > 0 {
				fmt.Printf("         %d moved to %s\n",
					len(unclaimed.Quarantined), filepath.Dir(unclaimed.Quarantined[0]))
			}
		}
	}

	// A persistent backlog — mail unclaimed long enough to be quarantined —
	// exits non-zero so a scheduler surfaces it. Freshly unclaimed files only
	// warn (exit 0): they are usually a network briefly misconfigured and will
	// toss once it is fixed, so failing on them would be noise.
	return hadErrors || len(unclaimed.Quarantined) > 0
}

// cmdScan implements 'v3mail scan': scan JAM bases for unsent echomail and create outbound .PKT files.
func cmdScan(args []string) {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	configDir := fs.String("config", "configs", "Config directory")
	dataDir := fs.String("data", "data", "Data directory")
	networkName := fs.String("network", "", "Limit to a single network (default: all enabled)")
	quiet := fs.Bool("q", false, "Quiet mode")
	_ = fs.Parse(args) // ExitOnError: Parse exits the program on failure

	ftnCfg, msgMgr, dupeDB, err := loadFTNDeps(*configDir, *dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if scanFTN(ftnCfg, msgMgr, dupeDB, *networkName, *quiet) {
		os.Exit(1)
	}
}

// scanFTN exports unsent echomail and netmail to outbound packets for every
// enabled network, or only networkName when set, and reports whether any
// network had errors.
func scanFTN(ftnCfg config.FTNConfig, msgMgr *message.MessageManager, dupeDB *tosser.DupeDB, networkName string, quiet bool) bool {
	totalExported := 0
	hadErrors := false

	for name, netCfg := range ftnCfg.Networks {
		if !netCfg.InternalTosserEnabled {
			continue
		}
		if networkName != "" && name != networkName {
			continue
		}

		t, err := tosser.New(name, netCfg, ftnCfg, dupeDB, msgMgr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating tosser for %s: %v\n", name, err)
			hadErrors = true
			continue
		}

		result := t.ScanAndExport()
		totalExported += result.MessagesExported

		if !quiet {
			fmt.Printf("[%s] scan: %d messages exported",
				name, result.MessagesExported)
			if len(result.Errors) > 0 {
				fmt.Printf(", %d errors", len(result.Errors))
			}
			fmt.Println()
		}
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "  [%s] ERROR: %s\n", name, e)
			hadErrors = true
		}
	}

	if !quiet {
		fmt.Printf("Scan complete: %d messages exported to outbound\n", totalExported)
	}
	return hadErrors
}

// cmdFtnPack implements 'v3mail ftn-pack': create ZIP bundles from staged .PKT files for binkd.
func cmdFtnPack(args []string) {
	fs := flag.NewFlagSet("ftn-pack", flag.ExitOnError)
	configDir := fs.String("config", "configs", "Config directory")
	dataDir := fs.String("data", "data", "Data directory")
	networkName := fs.String("network", "", "Limit to a single network (default: all enabled)")
	quiet := fs.Bool("q", false, "Quiet mode")
	_ = fs.Parse(args) // ExitOnError: Parse exits the program on failure

	ftnCfg, msgMgr, dupeDB, err := loadFTNDeps(*configDir, *dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if packFTN(ftnCfg, msgMgr, dupeDB, *networkName, *quiet) {
		os.Exit(1)
	}
}

// packFTN bundles staged outbound packets into each network's binkd outbound,
// for every enabled network or only networkName when set, and reports whether
// any network had errors.
func packFTN(ftnCfg config.FTNConfig, msgMgr *message.MessageManager, dupeDB *tosser.DupeDB, networkName string, quiet bool) bool {
	totalBundles, totalPackets := 0, 0
	hadErrors := false

	for name, netCfg := range ftnCfg.Networks {
		if !netCfg.InternalTosserEnabled {
			continue
		}
		if networkName != "" && name != networkName {
			continue
		}

		t, err := tosser.New(name, netCfg, ftnCfg, dupeDB, msgMgr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating tosser for %s: %v\n", name, err)
			hadErrors = true
			continue
		}

		result := t.PackOutbound()
		totalBundles += result.BundlesCreated
		totalPackets += result.PacketsPacked

		if !quiet {
			fmt.Printf("[%s] ftn-pack: %d bundles created (%d packets)",
				name, result.BundlesCreated, result.PacketsPacked)
			if len(result.Errors) > 0 {
				fmt.Printf(", %d errors", len(result.Errors))
			}
			fmt.Println()
		}
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "  [%s] ERROR: %s\n", name, e)
			hadErrors = true
		}
	}

	if !quiet {
		fmt.Printf("Pack complete: %d bundles created, %d packets packed\n", totalBundles, totalPackets)
	}
	return hadErrors
}

// loadFTNDeps loads all shared dependencies needed by toss/scan/ftn-pack commands.
// FTN paths in ftn.json are resolved relative to the BBS root (parent of dataDir)
// so toss/scan/pack work correctly regardless of CWD when v3mail is run.
func loadFTNDeps(configDir, dataDir string) (config.FTNConfig, *message.MessageManager, *tosser.DupeDB, error) {
	ftnCfg, err := config.LoadFTNConfig(configDir)
	if err != nil {
		return config.FTNConfig{}, nil, nil, fmt.Errorf("load ftn config: %w", err)
	}
	if err := config.ValidateFTNConfig(ftnCfg); err != nil {
		return config.FTNConfig{}, nil, nil, fmt.Errorf("ftn config invalid: %w", err)
	}

	// BBS root = directory containing the data folder (for resolving relative FTN paths)
	absData, err := filepath.Abs(dataDir)
	if err != nil {
		absData = dataDir
	}
	bbsRoot := filepath.Dir(absData)

	// Resolve relative FTN paths against BBS root
	ftnCfg.ResolvePaths(bbsRoot)

	// Build per-network origin line text for MessageManager
	origins := make(map[string]string)
	for name, net := range ftnCfg.Networks {
		origins[name] = net.Origin
	}

	// Load server config for board name
	serverCfg, err := config.LoadServerConfig(configDir)
	boardName := "Vision3 BBS"
	if err == nil {
		boardName = serverCfg.BoardName
	}

	msgMgr, err := message.NewMessageManager(dataDir, configDir, boardName, origins)
	if err != nil {
		return config.FTNConfig{}, nil, nil, fmt.Errorf("init message manager: %w", err)
	}

	// Load or create dupe database (path already resolved by ResolvePaths)
	dupeDBPath := ftnCfg.DupeDBPath
	if dupeDBPath == "" {
		dupeDBPath = filepath.Join(dataDir, "ftn", "dupes.json")
	}
	dupeDB, err := tosser.NewDupeDBFromPath(dupeDBPath)
	if err != nil {
		return config.FTNConfig{}, nil, nil, fmt.Errorf("load dupe db: %w", err)
	}

	return ftnCfg, msgMgr, dupeDB, nil
}

// loadFileAreas loads the file areas that inbound file echoes are delivered
// into. It returns nil — TICs are then left in the inbound — when the BBS has
// no file areas configured or they cannot be loaded.
func loadFileAreas(dataDir, configDir string) tosser.FileAreaStore {
	// Checked first because NewFileManager creates a missing file_areas.json,
	// and a mail run has no business doing that.
	if _, err := os.Stat(filepath.Join(configDir, "file_areas.json")); err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("inbound file echoes will not be delivered: cannot read file_areas.json", "error", err)
		}
		return nil
	}
	fm, err := file.NewFileManager(dataDir, configDir)
	if err != nil {
		slog.Warn("inbound file echoes will not be delivered: cannot load file areas", "error", err)
		return nil
	}
	return fm
}

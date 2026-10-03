package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// cmdFileEcho implements 'helper fileecho': create file areas for the file
// echoes in a network's file echo list, each linked to its echo so that
// v3mail toss delivers inbound TICs into it. The counterpart of ftnsetup for
// file echoes.
func cmdFileEcho(args []string) {
	fs := flag.NewFlagSet("fileecho", flag.ExitOnError)
	naFile := fs.String("na", "", "Path to the network's file echo list, e.g. tqw_file.na (required)")
	network := fs.String("network", "", "Network name in ftn.json the echoes come from (required)")
	tagPrefix := fs.String("tag-prefix", "", "Prefix for the new file areas' tags (default: none)")
	conferenceID := fs.Int("conference-id", 0, "Conference the new file areas belong to (0 = ungrouped)")
	acsList := fs.String("acs-list", "", "ACS string for listing files")
	acsDownload := fs.String("acs-download", "", "ACS string for downloading files")
	acsUpload := fs.String("acs-upload", "s250", "ACS string for uploading (file echo areas are fed by the network)")
	configDir := fs.String("config", "configs", "Config directory")
	dryRun := fs.Bool("dry-run", false, "Show what would be done without modifying files")

	fs.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: helper fileecho [options]\n\n")
		_, _ = fmt.Fprintf(os.Stderr, "Create file areas for the file echoes in a network's file echo list.\n")
		_, _ = fmt.Fprintf(os.Stderr, "Each area is linked to its echo, so v3mail toss delivers the files that\n")
		_, _ = fmt.Fprintf(os.Stderr, "arrive for it by TIC. Updates file_areas.json; echoes that already have\n")
		_, _ = fmt.Fprintf(os.Stderr, "an area are left alone, so it is safe to re-run with a newer list.\n\n")
		_, _ = fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(os.Stderr, "\nExample:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  helper fileecho --na tqw_file.na --network tqwnet --acs-list s10 --acs-download s20\n")
	}
	_ = fs.Parse(args) // ExitOnError: Parse exits the program on failure

	if *naFile == "" || *network == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Error: --na and --network are required\n\n")
		fs.Usage()
		os.Exit(1)
	}

	f, err := os.Open(*naFile)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	echoes, err := ftn.ParseFileEchoList(f)
	_ = f.Close() // read-only
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", *naFile, err)
		os.Exit(1)
	}

	ftnCfg, err := loadFTNConfig(filepath.Join(*configDir, "ftn.json"))
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error loading ftn.json: %v\n", err)
		os.Exit(1)
	}
	netName, ok := findNetwork(ftnCfg, *network)
	if !ok {
		_, _ = fmt.Fprintf(os.Stderr, "Error: network %q is not in ftn.json — set the network up first (helper ftnsetup)\n", *network)
		os.Exit(1)
	}
	// The network name becomes a directory under data/files.
	if err := file.CheckFilename(netName); err != nil || strings.Trim(netName, ".") == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Error: network name %q cannot be used as a directory name\n", netName)
		os.Exit(1)
	}
	if *conferenceID != 0 {
		if err := checkConference(filepath.Join(*configDir, "conferences.json"), *conferenceID); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	areasPath := filepath.Join(*configDir, "file_areas.json")
	areas, err := loadFileAreas(*configDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		areas = nil
	}

	added, linked := ftn.PlanFileEchoAreas(areas, echoes, ftn.FileEchoAreaOptions{
		Network:      netName,
		TagPrefix:    *tagPrefix,
		ConferenceID: *conferenceID,
		ACSList:      *acsList,
		ACSDownload:  *acsDownload,
		ACSUpload:    *acsUpload,
	})

	fmt.Printf("Parsed %d file echoes from %s\n", len(echoes), *naFile)
	for _, a := range linked {
		fmt.Printf("  HAVE  %-20s already fed to file area %s\n", a.FileEcho, a.Tag)
	}
	for _, a := range added {
		fmt.Printf("  ADD   %-20s -> file area %-20s %s\n", a.FileEcho, a.Tag, a.Name)
	}
	if len(added) == 0 {
		fmt.Println("Nothing to add.")
		return
	}
	if *dryRun {
		fmt.Printf("(dry run — %d file areas would be added to %s)\n", len(added), areasPath)
		return
	}

	if err := writeJSON(areasPath, append(areas, added...)); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", areasPath, err)
		os.Exit(1)
	}
	fmt.Printf("Added %d file areas to %s.\n", len(added), areasPath)
	fmt.Printf("Subscribe to the echoes at your %s hub, and set each link's tic_password if the hub uses one.\n", netName)
}

// checkConference reports an error unless conferences.json defines id.
func checkConference(path string, id int) error {
	confs, err := loadConferences(path)
	if err != nil {
		return fmt.Errorf("--conference-id %d: cannot read %s: %w", id, path, err)
	}
	for _, c := range confs {
		if c.ID == id {
			return nil
		}
	}
	return fmt.Errorf("--conference-id %d: no such conference in %s", id, path)
}

// findNetwork returns the ftn.json spelling of a network name, matched
// case-insensitively.
func findNetwork(cfg ftnConfig, name string) (string, bool) {
	for n := range cfg.Networks {
		if strings.EqualFold(n, name) {
			return n, true
		}
	}
	return "", false
}

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// cmdNodelist implements 'helper nodelist': compile a network's nodelist for
// lookups (import), and look systems up in the compiled lists (lookup).
// v3mail toss compiles the nodelists that arrive by file echo on its own;
// import is for the first list, a re-import, or a network whose nodelist does
// not come by file echo.
func cmdNodelist(args []string) {
	if len(args) < 1 {
		printNodelistHelp("")
		os.Exit(1)
	}
	switch strings.ToLower(args[0]) {
	case "import":
		cmdNodelistImport(args[1:])
	case "lookup":
		cmdNodelistLookup(args[1:])
	case "help", "--help", "-h":
		printNodelistHelp("")
	default:
		printNodelistHelp(fmt.Sprintf("Unknown subcommand: %s", args[0]))
		os.Exit(1)
	}
}

func printNodelistHelp(errMsg string) {
	w := os.Stderr
	printHeader()
	_, _ = fmt.Fprintln(w)
	if errMsg != "" {
		_, _ = fmt.Fprintln(w, bullet(errMsg))
	}
	_, _ = fmt.Fprintln(w, bullet("Required Format: helper nodelist <subcommand> [options]"))
	_, _ = fmt.Fprintln(w, bullet("Valid Subcommands Are As Follows..."))
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "  %sNodelist Subcommands:%s\n", clrBold, clrReset)
	_, _ = fmt.Fprintln(w, helpcmd("IMPORT", "Compile a network's nodelist from a file, a URL or the registry"))
	_, _ = fmt.Fprintln(w, helpcmd("LOOKUP <address>...", "Look systems up in the compiled nodelists"))
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, bullet("Run 'helper nodelist <subcommand> --help' for its options."))
	_, _ = fmt.Fprintln(w)
}

func cmdNodelistImport(args []string) {
	fs := flag.NewFlagSet("nodelist import", flag.ExitOnError)
	network := fs.String("network", "", "Network name in ftn.json (required)")
	fromFile := fs.String("file", "", "Nodelist file: plain text, or a ZIP or LHA archive holding it (e.g. FSXNET.Z75)")
	fromURL := fs.String("url", "", "Download the nodelist from this URL")
	fromRegistry := fs.Bool("registry", false, "Download the nodelist from the network registry's nodelist URL")
	force := fs.Bool("force", false, "Replace the compiled nodelist even if it is newer than this one")
	configDir := fs.String("config", "configs", "Config directory")
	dataDir := fs.String("data", "data", "Data directory")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: helper nodelist import --network NAME (--file PATH | --url URL | --registry) [options]\n\n")
		_, _ = fmt.Fprintf(os.Stderr, "Compile a network's nodelist into data/ftn/nodelist/<network>.json, which the\n")
		_, _ = fmt.Fprintf(os.Stderr, "BBS looks systems up in. v3mail toss does this by itself for a nodelist that\n")
		_, _ = fmt.Fprintf(os.Stderr, "arrives in the network's nodelist file echo (ftn.json: nodelist.file_echo).\n\n")
		_, _ = fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(os.Stderr, "\nExamples:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  helper nodelist import --network fsxnet --file data/files/fsxnet/fsx_node/FSXNET.Z75\n")
		_, _ = fmt.Fprintf(os.Stderr, "  helper nodelist import --network fidonet --registry\n")
	}
	_ = fs.Parse(args) // ExitOnError: Parse exits the program on failure

	sources := 0
	for _, set := range []bool{*fromFile != "", *fromURL != "", *fromRegistry} {
		if set {
			sources++
		}
	}
	if *network == "" || sources != 1 {
		_, _ = fmt.Fprintf(os.Stderr, "Error: --network and exactly one of --file, --url or --registry are required\n\n")
		fs.Usage()
		os.Exit(1)
	}

	ftnCfg, err := loadFTNConfig(filepath.Join(*configDir, "ftn.json"))
	if err != nil {
		fatalf("Error loading ftn.json: %v", err)
	}
	netName, ok := findNetwork(ftnCfg, *network)
	if !ok {
		fatalf("Error: network %q is not in ftn.json", *network)
	}
	own, err := ftn.ParseAddress(ftnCfg.Networks[netName].OwnAddress)
	if err != nil {
		fatalf("Error: network %s has no usable own_address: %v", netName, err)
	}

	var (
		nl     *ftn.Nodelist
		source string
	)
	switch {
	case *fromFile != "":
		source = filepath.Base(*fromFile)
		nl, err = ftn.ReadNodelistFile(*fromFile)
	default:
		url := *fromURL
		if *fromRegistry {
			if url, err = registryNodelistURL(*configDir, own.Zone); err != nil {
				fatalf("Error: %v", err)
			}
		}
		source = url
		fmt.Printf("Downloading %s\n", url)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		nl, err = ftn.DownloadNodelist(ctx, url)
		cancel()
	}
	if err != nil {
		fatalf("Error reading nodelist: %v", err)
	}

	compiled := ftn.CompileNodelist(nl, netName, source)
	if !compiled.HasZone(own.Zone) {
		fatalf("Error: %s has no entry for zone %d (%s's own address), so it is not this network's nodelist", source, own.Zone, netName)
	}
	dir := ftn.NodelistDir(*dataDir)
	saved, current, err := ftn.SaveCompiledNodelist(dir, netName, compiled, *force)
	if err != nil {
		fatalf("Error saving compiled nodelist: %v", err)
	}
	if !saved {
		fatalf("Not replaced: the compiled %s nodelist (%s, %s) is newer than %s (%s). Use --force to replace it anyway.",
			netName, current.Source, describeNodelistDate(current), source, describeNodelistDate(compiled))
	}
	path, _ := ftn.CompiledNodelistPath(dir, netName)
	fmt.Printf("Compiled %d systems from %s (%s) into %s\n", len(compiled.Nodes), source, describeNodelistDate(compiled), path)
}

// registryNodelistURL returns the nodelist URL the network registry has for a
// zone, preferring the sysop's ftn_networks.json to the built-in registry.
func registryNodelistURL(configDir string, zone int) (string, error) {
	override, err := ftn.LoadOverrideRegistry(configDir)
	if err != nil {
		return "", err
	}
	builtin, err := ftn.LoadRegistry()
	if err != nil {
		return "", err
	}
	for _, n := range append(override, builtin...) {
		if n.Zone == zone && n.NodelistURL != "" {
			return n.NodelistURL, nil
		}
	}
	return "", fmt.Errorf("the network registry has no nodelist URL for zone %d; use --file or --url", zone)
}

func describeNodelistDate(c *ftn.CompiledNodelist) string {
	if c.Date.IsZero() {
		return "undated"
	}
	if c.DayNumber > 0 {
		return fmt.Sprintf("%s, day %d", c.Date.Format("2006-01-02"), c.DayNumber)
	}
	return c.Date.Format("2006-01-02")
}

func cmdNodelistLookup(args []string) {
	fs := flag.NewFlagSet("nodelist lookup", flag.ExitOnError)
	network := fs.String("network", "", "Network to look in (default: every network in ftn.json)")
	configDir := fs.String("config", "configs", "Config directory")
	dataDir := fs.String("data", "data", "Data directory")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: helper nodelist lookup [options] <address>...\n\n")
		_, _ = fmt.Fprintf(os.Stderr, "Look systems up in the compiled nodelists. A point address shows its boss node.\n\n")
		_, _ = fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(os.Stderr, "\nExample:\n  helper nodelist lookup 21:4/158 1337:3/123.1\n")
	}
	_ = fs.Parse(args) // ExitOnError: Parse exits the program on failure
	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}

	var networks []string
	if *network != "" {
		networks = []string{*network}
	} else {
		ftnCfg, err := loadFTNConfig(filepath.Join(*configDir, "ftn.json"))
		if err != nil {
			fatalf("Error loading ftn.json: %v", err)
		}
		for n := range ftnCfg.Networks {
			networks = append(networks, n)
		}
		sort.Strings(networks)
	}

	index := ftn.NewNodelistIndex(ftn.NodelistDir(*dataDir))
	compiled := 0
	for _, n := range networks {
		if index.List(n) != nil {
			compiled++
		}
	}
	if compiled == 0 {
		fatalf("No compiled nodelists in %s. Run 'helper nodelist import' first.", ftn.NodelistDir(*dataDir))
	}

	missing := false
	for _, arg := range fs.Args() {
		addr, err := ftn.ParseAddress(arg)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", arg, err)
			missing = true
			continue
		}
		found := false
		for _, n := range networks {
			node, ok := index.Lookup(n, addr)
			if !ok {
				continue
			}
			found = true
			fmt.Printf("%s  [%s]\n", arg, n)
			if addr.Point != 0 {
				fmt.Printf("  Point of:  %s\n", node.Address)
			}
			fmt.Printf("  System:    %s\n", node.Name)
			if node.Location != "" {
				fmt.Printf("  Location:  %s\n", node.Location)
			}
			if node.Sysop != "" {
				fmt.Printf("  Sysop:     %s\n", node.Sysop)
			}
			if node.Status != "" {
				fmt.Printf("  Status:    %s\n", node.Status)
			}
			if len(node.Flags) > 0 {
				fmt.Printf("  Flags:     %s\n", strings.Join(node.Flags, ","))
			}
		}
		if !found {
			fmt.Printf("%s  not listed\n", arg)
			missing = true
		}
	}
	if missing {
		os.Exit(1)
	}
}

// fatalf prints an error and exits.
func fatalf(format string, a ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}

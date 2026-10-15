package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// --- areas command group ---
//
// Bulk maintenance of message areas. A sysop who imports a network's 200
// echoes with ftnsetup would otherwise have to open each one in the config
// editor to flip a setting such as Real Name Only; `helper areas set` applies
// the change to every area matching a filter in one pass. The running BBS
// picks up the rewritten message_areas.json through its config watcher.

func printAreasHelp(errMsg string) {
	w := os.Stderr
	printHeader()
	_, _ = fmt.Fprintln(w)
	if errMsg != "" {
		_, _ = fmt.Fprintln(w, bullet(errMsg))
	}
	_, _ = fmt.Fprintln(w, bullet("Required Format: helper areas <subcommand> [options]"))
	_, _ = fmt.Fprintln(w, bullet("Valid Subcommands Are As Follows..."))
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "  %sArea Subcommands:%s\n", clrBold, clrReset)
	_, _ = fmt.Fprintln(w, helpcmd("LIST", "List message areas matching a filter"))
	_, _ = fmt.Fprintln(w, helpcmd("SET", "Change settings on every message area matching a filter"))
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "  %sFilters (combined with AND):%s\n", clrBold, clrReset)
	_, _ = fmt.Fprintln(w, helpopt("--conference C", "Conference tag or ID"))
	_, _ = fmt.Fprintln(w, helpopt("--network NAME", "FTN/V3Net network key (e.g. fidonet)"))
	_, _ = fmt.Fprintln(w, helpopt("--type TYPE", "Area type: local, echomail, netmail, v3net, qwknet"))
	_, _ = fmt.Fprintln(w, helpopt("--tag PATTERN", "Area tag glob, case-insensitive (e.g. 'FD_*')"))
	_, _ = fmt.Fprintln(w, helpopt("--all", "Match every area (set requires a filter or --all)"))
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "  %sSet Options:%s\n", clrBold, clrReset)
	_, _ = fmt.Fprintln(w, helpopt("--real-name on|off", "Require real names for posts"))
	_, _ = fmt.Fprintln(w, helpopt("--anonymous on|off", "Allow anonymous posts"))
	_, _ = fmt.Fprintln(w, helpopt("--auto-join on|off", "Auto-join the area for new users"))
	_, _ = fmt.Fprintln(w, helpopt("--acs-read ACS", "ACS required to read"))
	_, _ = fmt.Fprintln(w, helpopt("--acs-write ACS", "ACS required to post"))
	_, _ = fmt.Fprintln(w, helpopt("--max-messages N", "Messages to retain (0 = unlimited)"))
	_, _ = fmt.Fprintln(w, helpopt("--max-age DAYS", "Purge messages older than N days (0 = unlimited)"))
	_, _ = fmt.Fprintln(w, helpopt("--sponsor HANDLE", "Area sponsor/moderator (empty to clear)"))
	_, _ = fmt.Fprintln(w, helpopt("--dry-run", "Show what would change without writing"))
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "  %sGlobal Options:%s\n", clrBold, clrReset)
	_, _ = fmt.Fprintln(w, helpopt("--config DIR", "Config directory (default: configs)"))
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "  %sExample:%s\n", clrBold, clrReset)
	_, _ = fmt.Fprintln(w, "  helper areas set --conference FIDONET --type echomail --real-name on --dry-run")
	_, _ = fmt.Fprintln(w)
}

func cmdAreas(args []string) {
	if len(args) < 1 {
		printAreasHelp("")
		os.Exit(1)
	}

	sub := args[0]
	switch strings.ToLower(sub) {
	case "list":
		os.Exit(runAreasList(args[1:]))
	case "set":
		os.Exit(runAreasSet(args[1:]))
	case "help", "--help", "-h":
		printAreasHelp("")
	default:
		printAreasHelp(fmt.Sprintf("Unknown subcommand: %s", sub))
		os.Exit(1)
	}
}

// areaFilter selects message areas. Empty fields match anything.
type areaFilter struct {
	conference string
	network    string
	areaType   string
	tagGlob    string
	all        bool
}

func (f *areaFilter) register(fs *flag.FlagSet) {
	fs.StringVar(&f.conference, "conference", "", "Conference tag or ID")
	fs.StringVar(&f.network, "network", "", "Network key")
	fs.StringVar(&f.areaType, "type", "", "Area type (local, echomail, netmail, v3net, qwknet)")
	fs.StringVar(&f.tagGlob, "tag", "", "Area tag glob, case-insensitive")
	fs.BoolVar(&f.all, "all", false, "Match every area")
}

func (f *areaFilter) empty() bool {
	return f.conference == "" && f.network == "" && f.areaType == "" && f.tagGlob == ""
}

// resolveConference turns a conference tag or numeric ID into its ID.
func resolveConference(confs []conferenceConfig, ref string) (int, error) {
	if id, err := strconv.Atoi(ref); err == nil {
		// 0 is the "ungrouped" pseudo-conference and has no entry.
		if id == 0 {
			return 0, nil
		}
		for _, c := range confs {
			if c.ID == id {
				return id, nil
			}
		}
		return 0, fmt.Errorf("no conference with ID %d", id)
	}
	for _, c := range confs {
		if strings.EqualFold(c.Tag, ref) {
			return c.ID, nil
		}
	}
	return 0, fmt.Errorf("no conference with tag %q", ref)
}

// matchAreas returns the indexes into areas that satisfy the filter.
func matchAreas(areas []message.MessageArea, confs []conferenceConfig, f areaFilter) ([]int, error) {
	confID := -1
	if f.conference != "" {
		id, err := resolveConference(confs, f.conference)
		if err != nil {
			return nil, err
		}
		confID = id
	}
	glob := strings.ToUpper(f.tagGlob)
	if glob != "" {
		if _, err := filepath.Match(glob, ""); err != nil {
			return nil, fmt.Errorf("bad --tag pattern %q: %w", f.tagGlob, err)
		}
	}

	var idx []int
	for i := range areas {
		a := &areas[i]
		if confID >= 0 && a.ConferenceID != confID {
			continue
		}
		if f.network != "" && !strings.EqualFold(a.Network, f.network) {
			continue
		}
		if f.areaType != "" && normalizeAreaType(a.AreaType) != normalizeAreaType(f.areaType) {
			continue
		}
		if glob != "" {
			if ok, _ := filepath.Match(glob, strings.ToUpper(a.Tag)); !ok {
				continue
			}
		}
		idx = append(idx, i)
	}
	return idx, nil
}

// normalizeAreaType folds "echo", which the tosser and areafix accept as an
// alias, into "echomail" so --type echomail matches both.
func normalizeAreaType(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	if t == "echo" {
		return "echomail"
	}
	return t
}

// parseAreasFlags parses args, returning an exit code and false when the
// command should stop: 0 for an explicit --help, 2 for a bad flag or a stray
// positional argument (the flag parser stops at the first one, so any filters
// after it would be silently ignored).
func parseAreasFlags(fs *flag.FlagSet, args []string) (int, bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, false
		}
		return 2, false
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "Error: unexpected argument %q\n\n", fs.Arg(0))
		fs.Usage()
		return 2, false
	}
	return 0, true
}

func loadAreaConfigs(configDir string) (string, []message.MessageArea, []conferenceConfig, error) {
	areasPath := filepath.Join(configDir, "message_areas.json")
	areas, err := loadAreas(areasPath)
	if err != nil {
		return "", nil, nil, fmt.Errorf("loading %s: %w", areasPath, err)
	}
	confsPath := filepath.Join(configDir, "conferences.json")
	confs, err := loadConferences(confsPath)
	if err != nil && !os.IsNotExist(err) {
		return "", nil, nil, fmt.Errorf("loading %s: %w", confsPath, err)
	}
	return areasPath, areas, confs, nil
}

func runAreasList(args []string) int {
	fs := flag.NewFlagSet("areas list", flag.ContinueOnError)
	var f areaFilter
	f.register(fs)
	configDir := fs.String("config", "configs", "Config directory")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: helper areas list [filters]\n\n")
		fs.PrintDefaults()
	}
	if rc, ok := parseAreasFlags(fs, args); !ok {
		return rc
	}

	_, areas, confs, err := loadAreaConfigs(*configDir)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	idx, err := matchAreas(areas, confs, f)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	confTags := make(map[int]string, len(confs))
	for _, c := range confs {
		confTags[c.ID] = c.Tag
	}
	sort.SliceStable(idx, func(i, j int) bool { return areas[idx[i]].Position < areas[idx[j]].Position })

	fmt.Printf("%-5s %-24s %-12s %-9s %-10s %-4s %-4s %s\n", "ID", "TAG", "CONFERENCE", "TYPE", "NETWORK", "REAL", "ANON", "NAME")
	for _, i := range idx {
		a := &areas[i]
		fmt.Printf("%-5d %-24s %-12s %-9s %-10s %-4s %-4s %s\n",
			a.ID, a.Tag, confTags[a.ConferenceID], a.AreaType, a.Network,
			yesNo(a.RealNameOnly), anonLabel(a.AllowAnon), a.Name)
	}
	fmt.Printf("\n%d area(s)\n", len(idx))
	return 0
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func anonLabel(p *bool) string {
	return yesNo(p != nil && *p)
}

// parseOnOff accepts the usual spellings of a boolean switch.
func parseOnOff(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "yes", "y", "true", "1":
		return true, nil
	case "off", "no", "n", "false", "0":
		return false, nil
	}
	return false, fmt.Errorf("expected on or off, got %q", s)
}

// areaChange is one setting applied to each matched area. apply reports
// whether the area actually changed.
type areaChange struct {
	desc  string
	apply func(a *message.MessageArea) bool
}

// buildAreaChanges turns the set flags the sysop actually passed into changes.
// Unpassed flags are left alone, so `--sponsor ""` clears a sponsor while
// omitting --sponsor keeps it.
func buildAreaChanges(fs *flag.FlagSet) ([]areaChange, error) {
	var changes []areaChange
	var errs []string
	fs.Visit(func(fl *flag.Flag) {
		val := fl.Value.String()
		switch fl.Name {
		case "real-name":
			on, err := parseOnOff(val)
			if err != nil {
				errs = append(errs, "--real-name: "+err.Error())
				return
			}
			changes = append(changes, areaChange{"real name only = " + yesNo(on), func(a *message.MessageArea) bool {
				if a.RealNameOnly == on {
					return false
				}
				a.RealNameOnly = on
				return true
			}})
		case "anonymous":
			on, err := parseOnOff(val)
			if err != nil {
				errs = append(errs, "--anonymous: "+err.Error())
				return
			}
			changes = append(changes, areaChange{"allow anonymous = " + yesNo(on), func(a *message.MessageArea) bool {
				if a.AllowAnon != nil && *a.AllowAnon == on {
					return false
				}
				// An unset AllowAnon already means no; don't churn the file
				// writing an explicit false.
				if a.AllowAnon == nil && !on {
					return false
				}
				v := on
				a.AllowAnon = &v
				return true
			}})
		case "auto-join":
			on, err := parseOnOff(val)
			if err != nil {
				errs = append(errs, "--auto-join: "+err.Error())
				return
			}
			changes = append(changes, areaChange{"auto join = " + yesNo(on), func(a *message.MessageArea) bool {
				if a.AutoJoin == on {
					return false
				}
				a.AutoJoin = on
				return true
			}})
		case "acs-read":
			changes = append(changes, stringChange("read ACS", val, func(a *message.MessageArea) *string { return &a.ACSRead }))
		case "acs-write":
			changes = append(changes, stringChange("write ACS", val, func(a *message.MessageArea) *string { return &a.ACSWrite }))
		case "sponsor":
			changes = append(changes, stringChange("sponsor", val, func(a *message.MessageArea) *string { return &a.Sponsor }))
		case "max-messages":
			c, err := intChange("max messages", val, func(a *message.MessageArea) *int { return &a.MaxMessages })
			if err != nil {
				errs = append(errs, "--max-messages: "+err.Error())
				return
			}
			changes = append(changes, c)
		case "max-age":
			c, err := intChange("max age", val, func(a *message.MessageArea) *int { return &a.MaxAge })
			if err != nil {
				errs = append(errs, "--max-age: "+err.Error())
				return
			}
			changes = append(changes, c)
		}
	})
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return changes, nil
}

func stringChange(name, val string, field func(*message.MessageArea) *string) areaChange {
	return areaChange{fmt.Sprintf("%s = %q", name, val), func(a *message.MessageArea) bool {
		p := field(a)
		if *p == val {
			return false
		}
		*p = val
		return true
	}}
}

func intChange(name, val string, field func(*message.MessageArea) *int) (areaChange, error) {
	n, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil || n < 0 {
		return areaChange{}, fmt.Errorf("expected a non-negative number, got %q", val)
	}
	return areaChange{fmt.Sprintf("%s = %d", name, n), func(a *message.MessageArea) bool {
		p := field(a)
		if *p == n {
			return false
		}
		*p = n
		return true
	}}, nil
}

func runAreasSet(args []string) int {
	fs := flag.NewFlagSet("areas set", flag.ContinueOnError)
	var f areaFilter
	f.register(fs)
	configDir := fs.String("config", "configs", "Config directory")
	dryRun := fs.Bool("dry-run", false, "Show what would change without writing")
	quiet := fs.Bool("quiet", false, "Only print the summary")
	fs.String("real-name", "", "Require real names: on or off")
	fs.String("anonymous", "", "Allow anonymous posts: on or off")
	fs.String("auto-join", "", "Auto-join for new users: on or off")
	fs.String("acs-read", "", "ACS required to read")
	fs.String("acs-write", "", "ACS required to post")
	fs.String("max-messages", "", "Messages to retain (0 = unlimited)")
	fs.String("max-age", "", "Purge messages older than N days (0 = unlimited)")
	fs.String("sponsor", "", "Area sponsor/moderator handle")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: helper areas set [filters] [settings]\n\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(os.Stderr, "\nExample:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  helper areas set --conference FIDONET --type echomail --real-name on --dry-run\n")
	}
	if rc, ok := parseAreasFlags(fs, args); !ok {
		return rc
	}

	if f.empty() && !f.all {
		_, _ = fmt.Fprintf(os.Stderr, "Error: give a filter (--conference, --network, --type, --tag) or --all\n\n")
		fs.Usage()
		return 2
	}
	if !f.empty() && f.all {
		_, _ = fmt.Fprintf(os.Stderr, "Error: --all cannot be combined with other filters\n")
		return 2
	}

	changes, err := buildAreaChanges(fs)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}
	if len(changes) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "Error: no settings given — nothing to change\n\n")
		fs.Usage()
		return 2
	}

	// Hold the areas lock from the read to the write, so a save by the
	// running BBS or the config editor in between is not overwritten.
	areasLock, err := message.LockAreasFile(filepath.Join(*configDir, "message_areas.json"))
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: locking message areas: %v\n", err)
		return 1
	}
	defer areasLock.Release()

	areasPath, areas, confs, err := loadAreaConfigs(*configDir)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	idx, err := matchAreas(areas, confs, f)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if len(idx) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "No message areas match the filter.\n")
		return 1
	}

	for _, c := range changes {
		fmt.Printf("Setting %s\n", c.desc)
	}
	changed := 0
	for _, i := range idx {
		a := &areas[i]
		touched := false
		for _, c := range changes {
			if c.apply(a) {
				touched = true
			}
		}
		if touched {
			changed++
			if !*quiet {
				fmt.Printf("  %-24s %s\n", a.Tag, a.Name)
			}
		}
	}

	verb := "Updated"
	if *dryRun {
		verb = "Would update"
	}
	fmt.Printf("%s %d of %d matching area(s); %d already set.\n", verb, changed, len(idx), len(idx)-changed)

	if *dryRun || changed == 0 {
		return 0
	}
	if _, err := message.WriteAreasFile(areasPath, areas); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", areasPath, err)
		return 1
	}
	fmt.Printf("Wrote %s\n", areasPath)
	return 0
}

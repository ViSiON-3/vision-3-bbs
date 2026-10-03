package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/version"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

const (
	doctorReset   = "\033[0m"
	doctorBold    = "\033[1m"
	doctorCyan    = "\033[36m"
	doctorMagenta = "\033[35m"
	doctorGreen   = "\033[32m"
	doctorYellow  = "\033[33m"
	doctorRed     = "\033[31m"
	doctorDim     = "\033[90m"
	doctorRule    = "────────────────────────────────────────────────────────────────────────"
)

type doctorSeverity string

const (
	doctorOK   doctorSeverity = "ok"
	doctorWarn doctorSeverity = "warning"
	doctorFail doctorSeverity = "failure"
	doctorInfo doctorSeverity = "info"
)

type doctorCheck struct {
	Name    string         `json:"name"`
	Status  doctorSeverity `json:"status"`
	Message string         `json:"message,omitempty"`
	Fix     string         `json:"fix,omitempty"`
}

type doctorReport struct {
	Root    string        `json:"root"`
	Checks  []doctorCheck `json:"checks"`
	Fixes   []doctorFix   `json:"fixes,omitempty"`
	FixMode bool          `json:"fixMode,omitempty"`
	DryRun  bool          `json:"dryRun,omitempty"`
	Summary struct {
		Warnings int `json:"warnings"`
		Failures int `json:"failures"`
	} `json:"summary"`
}

type doctorFix struct {
	Path    string `json:"path"`
	Action  string `json:"action"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func runDoctor(root string, jsonOutput, fix, dryRun bool, stdout, stderr io.Writer) int {
	if fix {
		rootAbs, err := filepath.Abs(root)
		if err == nil {
			root = rootAbs
		}
	}
	fixes := []doctorFix(nil)
	if fix {
		fixes = fixDoctorDirectories(root, dryRun)
	}
	report := inspectBoard(root)
	report.Fixes = fixes
	report.FixMode = fix
	report.DryRun = dryRun
	for _, check := range report.Checks {
		switch check.Status {
		case doctorWarn:
			report.Summary.Warnings++
		case doctorFail:
			report.Summary.Failures++
		}
	}

	if jsonOutput {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			_, _ = fmt.Fprintf(stderr, "vision3 doctor: %v\n", err)
			return 2
		}
	} else {
		if err := printDoctorText(stdout, report); err != nil {
			_, _ = fmt.Fprintf(stderr, "vision3 doctor: %v\n", err)
			return 2
		}
	}

	if report.Summary.Failures > 0 {
		return 2
	}
	if report.Summary.Warnings > 0 {
		return 1
	}
	return 0
}

func printDoctorText(w io.Writer, report doctorReport) error {
	output := doctorTextOutput{w: w}
	color := false
	if file, ok := w.(*os.File); ok && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" {
		color = term.IsTerminal(int(file.Fd()))
	}
	paint := func(code, value string) string {
		if !color {
			return value
		}
		return code + value + doctorReset
	}

	output.printf("  %s v%s\n",
		paint(doctorBold, "ViSiON/3 Doctor Utility"), version.Number)
	output.println(paint(doctorDim, doctorRule))
	output.printf("%s  %s\n\n", paint(doctorMagenta, "BOARD"), report.Root)
	if report.FixMode {
		label := "SAFE FIXES"
		if report.DryRun {
			label += " · PREVIEW ONLY"
		}
		output.printf("%s\n", paint(doctorCyan+doctorBold, "  "+label))
		if len(report.Fixes) == 0 {
			output.println("  No safe directory fixes are needed.")
		}
		for _, fix := range report.Fixes {
			status, tint := "FIXED", doctorGreen
			switch fix.Status {
			case "would-create":
				status, tint = "WOULD", doctorCyan
			case "skip":
				status, tint = "SKIP", doctorYellow
			case "failed":
				status, tint = "ERROR", doctorRed
			}
			output.printf("  %s  %s  %s\n", paint(tint+doctorBold, "["+status+"]"), fix.Action, fix.Path)
			if fix.Message != "" {
				output.printf("         %s\n", fix.Message)
			}
		}
		output.println()
	}

	sections := map[string][]doctorCheck{}
	sectionOrder := []string{"BOARD FILES", "CONFIGURATION", "NETWORK & MESSAGING", "SECURITY", "MENUS & DOORS"}
	for _, check := range report.Checks {
		section := doctorSection(check.Name)
		sections[section] = append(sections[section], check)
	}
	for _, section := range sectionOrder {
		checks := sections[section]
		if len(checks) == 0 {
			continue
		}
		output.printf("%s\n", paint(doctorCyan+doctorBold, "  "+section))
		for _, check := range checks {
			label, tint := "PASS", doctorGreen
			switch check.Status {
			case doctorWarn:
				label, tint = "WARN", doctorYellow
			case doctorFail:
				label, tint = "FAIL", doctorRed
			case doctorInfo:
				label, tint = "INFO", doctorCyan
			}
			output.printf("  %s  %s", paint(tint+doctorBold, "["+label+"]"), check.Name)
			if check.Message != "" {
				output.printf("  %s", check.Message)
			}
			output.println()
			if check.Fix != "" {
				output.printf("         %s %s\n", paint(doctorMagenta, "FIX →"), check.Fix)
			}
		}
		output.println()
	}

	passed := len(report.Checks) - report.Summary.Warnings - report.Summary.Failures
	output.println(paint(doctorDim, doctorRule))
	summary := fmt.Sprintf("%d passed  ·  %d warning(s)  ·  %d failure(s)", passed, report.Summary.Warnings, report.Summary.Failures)
	if report.Summary.Failures > 0 {
		output.printf("%s  %s\n", paint(doctorRed+doctorBold, "NEEDS ATTENTION"), summary)
	} else if report.Summary.Warnings > 0 {
		output.printf("%s  %s\n", paint(doctorYellow+doctorBold, "CHECK WARNINGS"), summary)
	} else {
		output.printf("%s  %s\n", paint(doctorGreen+doctorBold, "ALL CHECKS PASSED"), summary)
	}
	return output.err
}

type doctorTextOutput struct {
	w   io.Writer
	err error
}

func (o *doctorTextOutput) printf(format string, args ...any) {
	if o.err == nil {
		_, o.err = fmt.Fprintf(o.w, format, args...)
	}
}

func (o *doctorTextOutput) println(args ...any) {
	if o.err == nil {
		_, o.err = fmt.Fprintln(o.w, args...)
	}
}

var doctorDirectoryFixes = []string{
	"data/users",
	"data/logs",
	"data/msgbases",
	"data/files",
}

func fixDoctorDirectories(root string, dryRun bool) []doctorFix {
	var fixes []doctorFix
	for _, relative := range doctorDirectoryFixes {
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Stat(path)
		if err == nil {
			if !info.IsDir() {
				fixes = append(fixes, doctorFix{
					Path: relative, Action: "create directory", Status: "skip",
					Message: "a non-directory already exists at this path; inspect it manually",
				})
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			fixes = append(fixes, doctorFix{Path: relative, Action: "create directory", Status: "failed", Message: err.Error()})
			continue
		}
		if dryRun {
			fixes = append(fixes, doctorFix{Path: relative, Action: "create directory (mode 0750)", Status: "would-create"})
			continue
		}
		if err := os.MkdirAll(path, 0o750); err != nil {
			fixes = append(fixes, doctorFix{Path: relative, Action: "create directory", Status: "failed", Message: err.Error()})
			continue
		}
		fixes = append(fixes, doctorFix{Path: relative, Action: "create directory (mode 0750)", Status: "fixed"})
	}
	return fixes
}

func doctorSection(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, ".cfg") || strings.Contains(lower, ".mnu") || strings.HasPrefix(lower, "doors") || strings.HasPrefix(lower, "menus/"):
		return "MENUS & DOORS"
	case strings.HasPrefix(lower, "ip ") || strings.Contains(lower, "permissions") || strings.Contains(lower, "qwk api") || strings.Contains(lower, "ssh host key"):
		return "SECURITY"
	case strings.Contains(lower, "ftn") || strings.Contains(lower, "binkd") || strings.Contains(lower, "qwknet") || strings.Contains(lower, "qwk network") || strings.Contains(lower, "message area") || strings.Contains(lower, "message base") || strings.Contains(lower, "file area") || strings.Contains(lower, "conference") || strings.Contains(lower, "v3net"):
		return "NETWORK & MESSAGING"
	case strings.HasPrefix(lower, "configs/"):
		return "CONFIGURATION"
	default:
		return "BOARD FILES"
	}
}

func inspectBoard(root string) doctorReport {
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(previousLogger)

	absRoot, err := filepath.Abs(root)
	if err == nil {
		root = absRoot
	}
	report := doctorReport{Root: root}
	add := func(name string, status doctorSeverity, message, fix string) {
		report.Checks = append(report.Checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	}

	for _, preflight := range collectPreflightResults(root) {
		status := doctorOK
		fix := ""
		if !preflight.ok {
			status = doctorWarn
			fix = "Run the platform setup script or restore the missing path."
			if preflight.critical {
				status = doctorFail
			}
		}
		add(preflight.name, status, preflight.message, fix)
	}

	configDir := filepath.Join(root, "configs")
	jsonFiles, err := filepath.Glob(filepath.Join(configDir, "*.json"))
	if err != nil {
		add("configs/*.json", doctorFail, err.Error(), "Check the configuration directory path.")
	} else {
		sort.Strings(jsonFiles)
		for _, path := range jsonFiles {
			name := filepath.ToSlash(filepath.Join("configs", filepath.Base(path)))
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				add(name, doctorFail, readErr.Error(), "Check file permissions and restore the file if missing.")
				continue
			}
			if !json.Valid(data) {
				add(name, doctorFail, jsonLocationError(data), "Fix the JSON syntax at the reported line and column.")
				continue
			}
			add(name, doctorOK, "parses", "")
		}
	}

	serverCfg, serverErr := config.LoadServerConfig(configDir)
	if serverErr != nil {
		message := jsonFileError(filepath.Join(configDir, "config.json"), &config.ServerConfig{}, serverErr)
		message = "configs/config.json: " + message
		add("configs/config.json", doctorFail, message, "Fix the server configuration JSON and values.")
	} else {
		checkUnknownTopLevelKeys(filepath.Join(configDir, "config.json"), reflect.TypeOf(config.ServerConfig{}), add)
		if serverCfg.QWKAPI.Enabled {
			status := doctorWarn
			message := fmt.Sprintf("QWK API is enabled on %s; this feature is experimental", serverCfg.QWKAPI.ListenAddr())
			if serverCfg.QWKAPI.Host == "" || serverCfg.QWKAPI.Host == "0.0.0.0" || serverCfg.QWKAPI.Host == "::" {
				message += " and is bound to all interfaces"
			}
			if serverCfg.QWKAPI.CertFile == "" || serverCfg.QWKAPI.KeyFile == "" {
				message += " with an automatically generated self-signed certificate"
			}
			add("QWK API", status, message, "Disable qwkAPI.enabled unless you intend to expose this experimental API.")
		}
	}

	ftnCfg, ftnErr := config.LoadFTNConfig(configDir)
	if ftnErr != nil {
		message := jsonFileError(filepath.Join(configDir, "ftn.json"), &config.FTNConfig{}, ftnErr)
		message = "configs/ftn.json: " + message
		add("configs/ftn.json", doctorFail, message, "Fix the FTN configuration JSON and values.")
	} else {
		checkUnknownTopLevelKeys(filepath.Join(configDir, "ftn.json"), reflect.TypeOf(config.FTNConfig{}), add)
		ftnCfg.ResolvePaths(root)
		if err := config.ValidateFTNConfig(ftnCfg); err != nil {
			add("FTN configuration", doctorWarn, err.Error(), "Correct the FTN settings shown in the error.")
		} else {
			add("FTN configuration", doctorOK, "passes existing FTN validation", "")
		}
	}

	checkAreasAndConferences(configDir, root, ftnCfg, add)
	checkQWKNet(configDir, serverCfg, serverErr, root, add)
	checkV3Net(configDir, root, add)
	checkFTNRuntime(ftnCfg, root, add)
	if serverErr == nil {
		checkIPLists(serverCfg, root, add)
		checkUserAndSecurity(configDir, root, serverCfg, add)
		checkBadUserNames(configDir, add)
	}
	checkMenuFiles(filepath.Join(root, "menus", "v3"), configDir, add)
	checkDoors(configDir, root, add)
	checkWritablePaths(root, add)
	return report
}

func checkFTNRuntime(cfg config.FTNConfig, root string, add func(string, doctorSeverity, string, string)) {
	networks := make([]string, 0, len(cfg.Networks))
	for network := range cfg.Networks {
		networks = append(networks, network)
	}
	sort.Strings(networks)
	for _, network := range networks {
		networkCfg := cfg.Networks[network]
		if strings.TrimSpace(networkCfg.OwnAddress) == "" {
			add("FTN addresses", doctorWarn, fmt.Sprintf("network %q has no own_address", network), "Set this network's own_address in configs/ftn.json.")
		} else if err := ftn.ValidateAddress(networkCfg.OwnAddress); err != nil {
			add("FTN addresses", doctorWarn, fmt.Sprintf("network %q has invalid own_address %q: %v", network, networkCfg.OwnAddress, err), "Use a valid FTN address such as 21:1/100.")
		}
		seenLinks := map[string]bool{}
		for i, link := range networkCfg.Links {
			where := fmt.Sprintf("network %q link %d", network, i+1)
			address := strings.TrimSpace(link.Address)
			if address == "" {
				add("FTN links", doctorWarn, where+" has no address", "Set the hub's FTN address in configs/ftn.json.")
				continue
			}
			if err := ftn.ValidateAddress(address); err != nil {
				add("FTN links", doctorWarn, fmt.Sprintf("%s has invalid address %q: %v", where, address, err), "Use a valid FTN address such as 21:1/100.")
			}
			key := strings.ToLower(address)
			if seenLinks[key] {
				add("FTN links", doctorWarn, fmt.Sprintf("network %q lists link address %q more than once", network, address), "Keep one link entry per FTN address.")
			} else {
				seenLinks[key] = true
			}
			if link.Hostname != "" && (link.Port < 0 || link.Port > 65535) {
				add("FTN links", doctorWarn, fmt.Sprintf("%s has invalid BinkP port %d", where, link.Port), "Use a port from 1 to 65535, or leave it blank for the default.")
			}
			if fam, ok := config.NormalizeIPFamily(link.IPFamily); !ok {
				add("FTN links", doctorWarn, fmt.Sprintf("%s has unsupported ip_family %q", where, link.IPFamily), "Use ipv4, ipv6, or leave ip_family empty for automatic selection.")
			} else if link.Hostname != "" {
				if err := config.ValidateLinkIPFamily(link.Hostname, fam); err != nil {
					add("FTN links", doctorWarn, fmt.Sprintf("%s: %v", where, err), "Match ip_family to the address family of the configured host.")
				}
			}
		}
	}
	if cfg.Binkd.Enabled {
		binkdPath := cfg.Binkd.BinaryPath
		if !filepath.IsAbs(binkdPath) {
			binkdPath = filepath.Join(root, binkdPath)
		}
		info, err := os.Stat(binkdPath)
		if err != nil {
			add("binkd binary", doctorWarn, fmt.Sprintf("not found at %s", binkdPath), "Install binkd or correct binkd.binary_path in configs/ftn.json.")
		} else if !info.Mode().IsRegular() {
			add("binkd binary", doctorWarn, fmt.Sprintf("%s is not a regular file", binkdPath), "Point binkd.binary_path at the binkd executable.")
		} else if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
			add("binkd binary", doctorWarn, fmt.Sprintf("%s is not executable", binkdPath), "Make the binkd binary executable or correct binkd.binary_path.")
		} else {
			add("binkd binary", doctorOK, binkdPath, "")
		}
	}
	confPath := filepath.Join(root, "data", "ftn", "binkd.conf")
	data, err := os.ReadFile(confPath)
	if err != nil {
		if len(cfg.Networks) > 0 || cfg.Binkd.Enabled {
			add("binkd.conf", doctorWarn, fmt.Sprintf("cannot read %s: %v", confPath, err), "Run the FTN Setup Wizard or restore data/ftn/binkd.conf.")
		}
	} else if ftn.HasPlaceholders(string(data), root) {
		add("binkd.conf", doctorWarn, "still contains template placeholders", "Run the FTN Setup Wizard and fill in the network's identity and link settings.")
	} else {
		add("binkd.conf", doctorOK, "contains no template placeholders", "")
		checkBinkdAgreement(string(data), cfg, root, add)
	}
}

func checkBinkdAgreement(contents string, cfg config.FTNConfig, root string, add func(string, doctorSeverity, string, string)) {
	addresses, nodes, domains := map[string]string{}, map[string]string{}, map[string]string{}
	inbound, insecureInbound := "", ""
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "address":
			if len(fields) >= 2 {
				addresses[strings.ToLower(fields[1])] = fields[1]
			}
		case "node":
			if len(fields) >= 2 {
				nodes[strings.ToLower(fields[1])] = strings.Join(fields[2:], " ")
			}
		case "domain":
			if len(fields) >= 3 {
				domains[strings.ToLower(fields[1])] = fields[2]
			}
		case "inbound":
			inbound = fields[1]
		case "inbound-nonsecure":
			insecureInbound = fields[1]
		}
	}
	issues := 0
	expectedAddresses, expectedNodes, expectedDomains := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for network, netCfg := range cfg.Networks {
		if addr := strings.TrimSpace(netCfg.OwnAddress); addr != "" {
			key := strings.ToLower(addr + "@" + network)
			expectedAddresses[key] = true
			expectedDomains[strings.ToLower(network)] = true
			if _, ok := addresses[key]; !ok {
				add("binkd.conf agreement", doctorWarn, fmt.Sprintf("network %q own_address %q is missing from binkd.conf address directives", network, addr), "Run the FTN setup wizard to synchronize data/ftn/binkd.conf.")
				issues++
			}
		}
		if expected := cfg.BinkdOutboundFor(network); expected != "" {
			if got, ok := domains[strings.ToLower(network)]; !ok || filepath.Clean(resolveFromRoot(root, got)) != filepath.Clean(expected) {
				add("binkd.conf agreement", doctorWarn, fmt.Sprintf("network %q domain outbound path does not match configs/ftn.json (binkd.conf: %q, config: %q)", network, got, expected), "Run the FTN setup wizard to synchronize domain outbound paths.")
				issues++
			}
		}
		for _, link := range netCfg.Links {
			if strings.TrimSpace(link.Address) == "" {
				continue
			}
			if strings.TrimSpace(link.PacketPassword) == "" {
				add("FTN packet password", doctorWarn, fmt.Sprintf("network %q link %q has no packet password", network, link.Address), "Confirm the hub permits passwordless packets, or configure packet_password from the hub operator.")
				issues++
			}
			key := strings.ToLower(strings.TrimSpace(link.Address) + "@" + network)
			if link.HostPort() != "" {
				expectedNodes[key] = true
			}
			args, ok := nodes[key]
			if link.HostPort() == "" {
				continue
			}
			if !ok {
				add("binkd.conf agreement", doctorWarn, fmt.Sprintf("network %q link %q has no matching node directive", network, link.Address), "Run the FTN setup wizard to synchronize link nodes.")
				issues++
				continue
			}
			fields := strings.Fields(args)
			if link.Hostname != "" && len(fields) >= 2 && !strings.EqualFold(fields[len(fields)-2], link.HostPort()) {
				// Node syntax may include an address-family option before host:port.
				add("binkd.conf agreement", doctorWarn, fmt.Sprintf("link %q node host/port %q does not match configs/ftn.json %q", link.Address, fields[len(fields)-2], link.HostPort()), "Run the FTN setup wizard to synchronize BinkP host settings.")
				issues++
			}
			wantPassword := link.SessionPassword
			if wantPassword == "" {
				wantPassword = "-"
			}
			if len(fields) == 0 || fields[len(fields)-1] != wantPassword {
				add("binkd.conf agreement", doctorWarn, fmt.Sprintf("link %q node session password does not match configs/ftn.json", link.Address), "Synchronize the BinkP session password in the FTN setup wizard; the packet password is separate.")
				issues++
			}
		}
	}
	for key := range addresses {
		if !expectedAddresses[key] {
			add("binkd.conf agreement", doctorWarn, fmt.Sprintf("binkd.conf address %q has no matching own_address in configs/ftn.json", addresses[key]), "Remove the stale address or restore the matching FTN network configuration.")
			issues++
		}
	}
	for key := range nodes {
		if !expectedNodes[key] {
			add("binkd.conf agreement", doctorWarn, fmt.Sprintf("binkd.conf node %q has no matching link in configs/ftn.json", key), "Remove the stale node directive or restore the matching link configuration.")
			issues++
		}
	}
	for key := range domains {
		if !expectedDomains[key] {
			add("binkd.conf agreement", doctorWarn, fmt.Sprintf("binkd.conf domain %q has no matching FTN network", key), "Remove the stale domain or restore the matching FTN network configuration.")
			issues++
		}
	}
	wantSecure := filepath.Join(root, "data", "ftn", "secure_in")
	if cfg.SecureInboundPath != "" {
		wantSecure = cfg.SecureInboundPath
	}
	wantInbound := cfg.InboundPath
	if got := strings.TrimSpace(inbound); got == "" {
		add("binkd.conf agreement", doctorWarn, "binkd.conf is missing the inbound directive", "Run the FTN setup wizard to restore inbound paths.")
		issues++
	} else if filepath.Clean(resolveFromRoot(root, got)) != filepath.Clean(wantSecure) {
		add("binkd.conf agreement", doctorWarn, fmt.Sprintf("inbound path %q does not match configured secure inbound path %q", got, wantSecure), "Run the FTN setup wizard to synchronize inbound paths.")
		issues++
	}
	if wantInbound != "" && strings.TrimSpace(insecureInbound) == "" {
		add("binkd.conf agreement", doctorWarn, "binkd.conf is missing the inbound-nonsecure directive", "Run the FTN setup wizard to restore inbound paths.")
		issues++
	} else if got := strings.TrimSpace(insecureInbound); got != "" && wantInbound != "" && filepath.Clean(resolveFromRoot(root, got)) != filepath.Clean(wantInbound) {
		add("binkd.conf agreement", doctorWarn, fmt.Sprintf("inbound-nonsecure path %q does not match configured inbound path %q", got, wantInbound), "Run the FTN setup wizard to synchronize inbound paths.")
		issues++
	}
	if issues == 0 {
		add("binkd.conf agreement", doctorOK, "addresses, links, passwords, and configured paths agree", "")
	}
}

func checkIPLists(cfg config.ServerConfig, root string, add func(string, doctorSeverity, string, string)) {
	for _, item := range []struct{ name, path string }{
		{name: "IP blocklist", path: cfg.IPBlocklistPath},
		{name: "IP allowlist", path: cfg.IPAllowlistPath},
	} {
		if strings.TrimSpace(item.path) == "" {
			continue
		}
		path := resolveFromRoot(root, item.path)
		data, err := os.ReadFile(path)
		if err != nil {
			add(item.name, doctorWarn, fmt.Sprintf("cannot read %s: %v", path, err), "Restore the configured IP list or clear its path in config.json.")
			continue
		}
		invalid := 0
		for i, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.Contains(line, "/") {
				if _, _, err := net.ParseCIDR(line); err != nil {
					add(item.name, doctorWarn, fmt.Sprintf("%s:%d has invalid CIDR %q", path, i+1, line), "Correct or remove the invalid address range.")
					invalid++
				}
			} else if net.ParseIP(line) == nil {
				add(item.name, doctorWarn, fmt.Sprintf("%s:%d has invalid IP address %q", path, i+1, line), "Correct or remove the invalid address.")
				invalid++
			}
		}
		if invalid == 0 {
			add(item.name, doctorOK, fmt.Sprintf("%s parses", path), "")
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	sshPath := filepath.Join(root, "configs", "ssh_host_rsa_key")
	if info, err := os.Stat(sshPath); err == nil && info.Mode().Perm()&0o077 != 0 {
		add("SSH host key permissions", doctorWarn, fmt.Sprintf("%s is accessible by group or other users (%04o)", sshPath, info.Mode().Perm()), "Restrict the host key to its owner, for example chmod 600 configs/ssh_host_rsa_key.")
	}
	for _, file := range []string{"ftn.json", "doors.json", "qwknet.json"} {
		path := filepath.Join(root, "configs", file)
		if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o004 != 0 {
			add(filepath.ToSlash(filepath.Join("configs", file))+" permissions", doctorWarn, "file is readable by all local users and may contain network credentials", "Restrict file permissions to the BBS operator, for example chmod 600 "+filepath.ToSlash(filepath.Join("configs", file))+".")
		}
	}
}

func checkUserAndSecurity(configDir, root string, cfg config.ServerConfig, add func(string, doctorSeverity, string, string)) {
	if cfg.SysOpLevel <= cfg.CoSysOpLevel {
		add("ACS access levels", doctorWarn, fmt.Sprintf("sysOpLevel %d should be higher than coSysOpLevel %d; SYSOP and COSYSOP will overlap", cfg.SysOpLevel, cfg.CoSysOpLevel), "Set sysOpLevel above coSysOpLevel in configs/config.json.")
	} else {
		add("ACS access levels", doctorOK, fmt.Sprintf("SYSOP uses level %d and COSYSOP uses level %d", cfg.SysOpLevel, cfg.CoSysOpLevel), "")
	}
	if cfg.AllowNewUsers {
		add("new user settings", doctorInfo, fmt.Sprintf("signups are enabled at level %d; regular users are level %d; logon threshold is %d; auto-validation is %t", cfg.NewUserLevel, cfg.RegularUserLevel, cfg.LogonLevel, cfg.AutoValidateNewUsers), "Confirm these levels and the validation policy match your signup workflow.")
		if cfg.NewUserLevel < cfg.LogonLevel {
			add("new user settings", doctorWarn, fmt.Sprintf("new users start at level %d, below the logon threshold %d", cfg.NewUserLevel, cfg.LogonLevel), "Raise newUserLevel or lower logonLevel if new signups should be able to log in immediately.")
		}
	} else {
		add("new user settings", doctorInfo, "new signups are disabled", "Enable allowNewUsers when you are ready to accept registrations.")
	}
	checkInfoFormSetup(configDir, root, cfg, add)
	checkDefaultSysopPassword(root, cfg, add)
}

func checkBadUserNames(configDir string, add func(string, doctorSeverity, string, string)) {
	path := filepath.Join(configDir, "badusers.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		add("bad user names", doctorWarn, fmt.Sprintf("configs/badusers.txt is missing or unreadable: %v", err), "Restore configs/badusers.txt; the companion signup check uses it to block reserved handles.")
		return
	}
	count := 0
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		count++
	}
	if count == 0 {
		add("bad user names", doctorWarn, "configs/badusers.txt has no blocked names", "Add at least one reserved handle pattern, one per line.")
		return
	}
	add("bad user names", doctorOK, fmt.Sprintf("configs/badusers.txt contains %d blocked name pattern(s)", count), "")
}

func checkInfoFormSetup(configDir, root string, cfg config.ServerConfig, add func(string, doctorSeverity, string, string)) {
	path := filepath.Join(root, "data", "infoforms", "config.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if cfg.UseNUV && cfg.NUVForm > 0 {
			add("InfoForms", doctorWarn, fmt.Sprintf("NUV displays form %d but %s is missing", cfg.NUVForm, filepath.ToSlash(filepath.Join("data", "infoforms", "templates", fmt.Sprintf("form_%d.txt", cfg.NUVForm)))), "Restore that InfoForm template or set nuvForm to 0 in configs/config.json.")
		}
		return
	}
	if err != nil {
		add("InfoForms", doctorWarn, "cannot read data/infoforms/config.json: "+err.Error(), "Check the file permissions and restore the InfoForms configuration if needed.")
		return
	}
	var formCfg menu.InfoFormConfig
	if err := json.Unmarshal(data, &formCfg); err != nil {
		add("InfoForms", doctorFail, "data/infoforms/config.json: "+jsonErrorLocation(data, err), "Fix the InfoForms configuration JSON.")
		return
	}
	requiredByLogin := false
	sequence, loginErr := config.LoadLoginSequence(configDir)
	if loginErr == nil {
		for _, item := range sequence {
			if strings.EqualFold(item.Command, "INFOFORMREQUIRED") {
				requiredByLogin = true
			}
		}
	}
	issues := 0
	forms := strings.TrimSpace(formCfg.RequiredForms)
	if forms != "" && !requiredByLogin {
		add("InfoForms", doctorWarn, "required_forms is set but login.json does not run INFOFORMREQUIRED", "Add INFOFORMREQUIRED to configs/login.json if users must complete these forms during login.")
		issues++
	}
	checked := map[int]bool{}
	for _, r := range forms {
		if r < '1' || r > '5' {
			add("InfoForms", doctorWarn, fmt.Sprintf("required_forms contains invalid form number %q", r), "Use only form numbers 1 through 5 in data/infoforms/config.json.")
			issues++
			continue
		}
		n := int(r - '0')
		if checked[n] {
			continue
		}
		checked[n] = true
		template := filepath.Join(root, "data", "infoforms", "templates", fmt.Sprintf("form_%d.txt", n))
		if info, statErr := os.Stat(template); statErr != nil || !info.Mode().IsRegular() {
			add("InfoForms", doctorWarn, fmt.Sprintf("required form %d template is missing: %s", n, template), "Restore or create the required form template.")
			issues++
		}
	}
	if cfg.UseNUV && cfg.NUVForm > 0 {
		template := filepath.Join(root, "data", "infoforms", "templates", fmt.Sprintf("form_%d.txt", cfg.NUVForm))
		if cfg.NUVForm > 5 {
			add("InfoForms", doctorWarn, fmt.Sprintf("nuvForm %d is outside the supported range 1–5", cfg.NUVForm), "Set nuvForm to a form number from 1 through 5, or 0 to disable.")
			issues++
		} else if _, statErr := os.Stat(template); statErr != nil {
			add("InfoForms", doctorWarn, fmt.Sprintf("NUV form template is missing: %s", template), "Restore the selected InfoForm template or disable NUV form display.")
			issues++
		}
	}
	if issues == 0 && (forms != "" || (cfg.UseNUV && cfg.NUVForm > 0)) {
		add("InfoForms", doctorOK, "configured required and NUV forms have templates", "")
	}
}

func checkDefaultSysopPassword(root string, cfg config.ServerConfig, add func(string, doctorSeverity, string, string)) {
	path := filepath.Join(root, "data", "users", "users.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var users []struct {
		Handle       string `json:"handle"`
		AccessLevel  int    `json:"accessLevel"`
		PasswordHash string `json:"passwordHash"`
	}
	if json.Unmarshal(data, &users) != nil {
		return
	}
	for _, u := range users {
		if u.AccessLevel < cfg.SysOpLevel || !strings.EqualFold(strings.TrimSpace(u.Handle), "Felonius") {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("password")) == nil {
			add("default sysop credentials", doctorWarn, "Felonius still uses the published default password", "Log in and change the password, or update the account with ./ue.")
			return
		}
	}
	add("default sysop credentials", doctorOK, "no published default sysop password detected", "")
}

func checkWritablePaths(root string, add func(string, doctorSeverity, string, string)) {
	for _, rel := range []string{"data", "data/users", "data/logs", "data/msgbases", "data/files"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		if !doctorPathWritable(info) {
			add("writable paths", doctorWarn, fmt.Sprintf("%s does not appear writable by the account running the doctor", rel), "Grant the BBS service account write and execute access to this directory.")
		}
	}
}

func checkMenuFiles(menuRoot, configDir string, add func(string, doctorSeverity, string, string)) {
	count := 0
	menuCFGs, menuMNU := make(map[string]bool), make(map[string]bool)
	type menuCommand struct{ file, command string }
	var menuCommands []menuCommand
	err := filepath.WalkDir(menuRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToUpper(filepath.Ext(entry.Name()))
		if ext == ".CFG" || ext == ".MNU" {
			key := strings.ToUpper(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
			if ext == ".CFG" {
				menuCFGs[key] = true
			} else {
				menuMNU[key] = true
			}
		}
		if ext != ".CFG" && ext != ".MNU" {
			return nil
		}
		count++
		data, err := os.ReadFile(path)
		name, _ := filepath.Rel(filepath.Dir(menuRoot), path)
		name = filepath.ToSlash(name)
		if err != nil {
			add(name, doctorFail, err.Error(), "Check file permissions and restore the menu file if missing.")
			return nil
		}
		var parseErr error
		switch ext {
		case ".CFG":
			if len(data) != 0 {
				var records []menu.CommandRecord
				parseErr = json.Unmarshal(data, &records)
				if parseErr == nil {
					for _, command := range records {
						menuCommands = append(menuCommands, menuCommand{file: name, command: command.Command})
					}
				}
			}
		case ".MNU":
			var menuRecord menu.MenuRecord
			parseErr = json.Unmarshal(data, &menuRecord)
		}
		if parseErr != nil {
			add(name, doctorFail, jsonErrorLocation(data, parseErr), "Fix the menu data at the reported line and column.")
			return nil
		}
		add(name, doctorOK, "parses", "")
		return nil
	})
	if err != nil {
		add("menus/v3", doctorFail, err.Error(), "Check the menu directory and its permissions.")
	} else if count == 0 {
		add("menus/v3", doctorWarn, "no .CFG or .MNU files found", "Restore the menu set or run setup.")
	}
	areas := loadDoctorAreaTags(configDir)
	doors, _ := config.LoadDoors(filepath.Join(configDir, "doors.json"))
	issues := 0
	for _, item := range menuCommands {
		parts := strings.SplitN(strings.TrimSpace(item.command), ":", 2)
		if len(parts) != 2 {
			action := strings.ToUpper(strings.TrimSpace(item.command))
			switch action {
			case "LOGOFF", "LOGIN", "NEWUSER", "CHECKACCESS", "DISCONNECT", "QUIT":
			default:
				add("menu references", doctorWarn, fmt.Sprintf("%s references unknown command action %q", item.file, item.command), "Correct the command to a supported ViSiON/3 menu action.")
				issues++
			}
			continue
		}
		action, target := strings.ToUpper(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1])
		missing := ""
		switch action {
		case "GOTO":
			if !menuCFGs[strings.ToUpper(target)] || !menuMNU[strings.ToUpper(target)] {
				missing = fmt.Sprintf("menu %q", target)
			}
		case "DOOR":
			doorName := strings.Fields(target)
			if len(doorName) == 0 {
				missing = "door with an empty name"
			} else if _, ok := doors[strings.ToUpper(doorName[0])]; !ok {
				missing = fmt.Sprintf("door %q", doorName[0])
			}
		case "RUN":
			runTarget := strings.Fields(target)
			if len(runTarget) == 0 {
				missing = "RUN target with an empty name"
			} else if !menu.IsKnownRunnableTarget(runTarget[0]) {
				missing = fmt.Sprintf("RUN target %q", runTarget[0])
			}
		case "MSGAREA":
			if !areas[strings.ToLower(target)] {
				missing = fmt.Sprintf("message area %q", target)
			}
		default:
			missing = fmt.Sprintf("command action %q", action)
		}
		if missing != "" {
			add("menu references", doctorWarn, fmt.Sprintf("%s references missing %s", item.file, missing), "Correct the command or add the referenced menu, door, runnable, or message area.")
			issues++
		}
	}
	if count > 0 && issues == 0 {
		add("menu references", doctorOK, "GOTO, DOOR, RUN, and MSGAREA targets resolve", "")
	}
}

func loadDoctorAreaTags(configDir string) map[string]bool {
	data, err := os.ReadFile(filepath.Join(configDir, "message_areas.json"))
	if err != nil {
		return nil
	}
	var areas []message.MessageArea
	if json.Unmarshal(data, &areas) != nil {
		return nil
	}
	tags := make(map[string]bool, len(areas))
	for _, area := range areas {
		tags[strings.ToLower(strings.TrimSpace(area.Tag))] = true
	}
	return tags
}

func checkDoors(configDir, root string, add func(string, doctorSeverity, string, string)) {
	path := filepath.Join(configDir, "doors.json")
	doors, err := config.LoadDoors(path)
	if err != nil {
		message := jsonFileError(path, &[]config.DoorConfig{}, err)
		message = "configs/doors.json: " + message
		add("doors", doctorFail, message, "Fix configs/doors.json.")
		return
	}
	issues := 0
	codes := make([]string, 0, len(doors))
	for code := range doors {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		door := doors[code]
		if err := door.ValidateRemote(); err != nil {
			add("doors", doctorWarn, err.Error(), "Correct the remote door settings.")
			issues++
			continue
		}
		if door.IsRemote() {
			continue
		}
		if door.Type == "synchronet_js" || door.Type == "v3_script" {
			script := door.Script
			if !filepath.IsAbs(script) {
				script = filepath.Join(door.WorkingDirectory, script)
			}
			if script != "" && !fileExists(resolveFromRoot(root, script)) {
				add("doors", doctorWarn, fmt.Sprintf("door %q script does not exist: %s", code, script), "Correct the script path or restore the script file.")
				issues++
			}
			continue
		}
		if len(door.Commands) == 0 || door.UseShell {
			continue
		}
		executable := door.Commands[0]
		if filepath.IsAbs(executable) || strings.ContainsAny(executable, `/\\`) {
			path := executable
			if !filepath.IsAbs(path) {
				path = filepath.Join(door.WorkingDirectory, path)
			}
			if !fileExists(resolveFromRoot(root, path)) {
				add("doors", doctorWarn, fmt.Sprintf("door %q executable does not exist: %s", code, path), "Correct the command path or restore the executable.")
				issues++
			}
		}
	}
	if issues == 0 {
		add("doors", doctorOK, fmt.Sprintf("%d door(s) load and remote settings are valid", len(doors)), "")
	}
}

func resolveFromRoot(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, path)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func checkAreasAndConferences(configDir, root string, ftnCfg config.FTNConfig, add func(string, doctorSeverity, string, string)) {
	areasPath := filepath.Join(configDir, "message_areas.json")
	areaData, err := os.ReadFile(areasPath)
	if err != nil {
		return // Preflight already reports this optional file.
	}
	var areas []message.MessageArea
	if len(areaData) > 0 {
		if err := json.Unmarshal(areaData, &areas); err != nil {
			add("message areas", doctorFail, "configs/message_areas.json: "+jsonErrorLocation(areaData, err), "Fix configs/message_areas.json.")
			return
		}
	}
	conferences := make(map[int]bool)
	confData, err := os.ReadFile(filepath.Join(configDir, "conferences.json"))
	if err == nil && len(confData) > 0 {
		var confs []conference.Conference
		if err := json.Unmarshal(confData, &confs); err != nil {
			add("conferences", doctorFail, "configs/conferences.json: "+jsonErrorLocation(confData, err), "Fix configs/conferences.json.")
			return
		}
		for _, conf := range confs {
			conferences[conf.ID] = true
		}
	}
	seenTags, seenBases := map[string]string{}, map[string]string{}
	areaTags := make(map[string]bool, len(areas))
	for _, area := range areas {
		areaTags[strings.ToLower(strings.TrimSpace(area.Tag))] = true
	}
	ftnNetworks := make(map[string]bool, len(ftnCfg.Networks))
	for key := range ftnCfg.Networks {
		ftnNetworks[strings.ToLower(key)] = true
	}
	qwkCfg, _ := config.LoadQWKNetConfig(configDir)
	qwkNetworks := make(map[string]bool, len(qwkCfg.Networks))
	qwkConferences := make(map[string]map[int]string)
	for key := range qwkCfg.Networks {
		qwkNetworks[strings.ToLower(key)] = true
		qwkConferences[strings.ToLower(key)] = make(map[int]string)
	}
	v3Cfg, _ := config.LoadV3NetConfig(configDir)
	v3Networks := make(map[string]bool)
	for _, network := range v3Cfg.Hub.Networks {
		v3Networks[strings.ToLower(strings.TrimSpace(network.Name))] = true
	}
	issues := 0
	for _, leaf := range v3Cfg.Leaves {
		v3Networks[strings.ToLower(strings.TrimSpace(leaf.Network))] = true
		for _, board := range leaf.Boards {
			if !areaTags[strings.ToLower(strings.TrimSpace(board))] {
				add("message areas", doctorWarn, fmt.Sprintf("V3Net leaf %q board %q does not match a local message area tag", leaf.Network, board), "Correct the board tag in configs/v3net.json or add the corresponding message area.")
				issues++
			}
		}
	}
	for _, area := range areas {
		if area.ConferenceID != 0 && !conferences[area.ConferenceID] {
			add("message areas", doctorFail, fmt.Sprintf("area %q refers to missing conference %d", area.Tag, area.ConferenceID), "Add the conference or change the area's conference_id.")
			issues++
		}
		tag := strings.ToLower(strings.TrimSpace(area.Tag))
		if tag != "" {
			if previous, ok := seenTags[tag]; ok {
				add("message areas", doctorFail, fmt.Sprintf("areas %q and %q share tag %q", previous, area.Tag, area.Tag), "Give each message area a unique tag.")
				issues++
			} else {
				seenTags[tag] = area.Tag
			}
		}
		base := strings.ToLower(filepath.Clean(strings.TrimSpace(area.BasePath)))
		if base != "" && base != "." {
			if previous, ok := seenBases[base]; ok {
				add("message areas", doctorFail, fmt.Sprintf("areas %q and %q share JAM base path %q", previous, area.Tag, area.BasePath), "Give each area a unique base_path.")
				issues++
			} else {
				seenBases[base] = area.Tag
			}
		}
		switch strings.ToLower(strings.TrimSpace(area.AreaType)) {
		case "echomail", "netmail":
			if area.Network == "" {
				add("message areas", doctorWarn, fmt.Sprintf("%s area %q has no FTN network", area.AreaType, area.Tag), "Choose a network in the area's configuration, or change its area_type if it is local.")
				issues++
			} else if !ftnNetworks[strings.ToLower(area.Network)] {
				add("message areas", doctorWarn, fmt.Sprintf("%s area %q references unknown FTN network %q", area.AreaType, area.Tag, area.Network), "Add that network in configs/ftn.json or correct the area's network field.")
				issues++
			} else if len(lookupFTNNetwork(ftnCfg.Networks, area.Network).Links) == 0 {
				add("message areas", doctorWarn, fmt.Sprintf("%s area %q uses FTN network %q, which has no links", area.AreaType, area.Tag, area.Network), "Add at least one hub/link under that network in configs/ftn.json.")
				issues++
			}
		case message.AreaTypeQWKNet:
			if area.Network == "" || !qwkNetworks[strings.ToLower(area.Network)] {
				add("message areas", doctorWarn, fmt.Sprintf("QWKnet area %q references unconfigured network %q", area.Tag, area.Network), "Add the network in configs/qwknet.json or correct the area's network field.")
				issues++
			} else if !qwkCfg.Networks[lookupKey(qwkCfg.Networks, area.Network)].Enabled {
				add("message areas", doctorWarn, fmt.Sprintf("QWKnet area %q is attached to disabled network %q", area.Tag, area.Network), "Enable the network in configs/qwknet.json or change the area's network field.")
				issues++
			}
			if area.QWKConference <= 0 {
				add("message areas", doctorWarn, fmt.Sprintf("QWKnet area %q has no valid qwk_conference number", area.Tag), "Set qwk_conference to the conference number assigned by the hub.")
				issues++
			} else if area.Network != "" && qwkNetworks[strings.ToLower(area.Network)] {
				networkKey := lookupKey(qwkCfg.Networks, area.Network)
				seen := qwkConferences[strings.ToLower(networkKey)]
				if previous, exists := seen[area.QWKConference]; exists {
					add("message areas", doctorWarn, fmt.Sprintf("QWKnet areas %q and %q both map to conference %d on %q", previous, area.Tag, area.QWKConference, networkKey), "Give each area the conference number published by the hub; each number must map to one local area per network.")
					issues++
				} else {
					seen[area.QWKConference] = area.Tag
				}
			}
		case "v3net":
			if area.Network == "" || !v3Networks[strings.ToLower(area.Network)] {
				add("message areas", doctorWarn, fmt.Sprintf("V3Net area %q references network %q, which is not listed in v3net.json", area.Tag, area.Network), "Add the network to the hub or leaves list in configs/v3net.json, or correct the area's network field.")
				issues++
			}
		}
		if base := strings.TrimSpace(area.BasePath); base != "" {
			path := base
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, "data", path)
			}
			if info, statErr := os.Stat(path); statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					add("message bases", doctorInfo, fmt.Sprintf("area %q base directory is not created yet: %s", area.Tag, path), "The message base is created when the area is first opened; check this path if messages fail to load.")
					parent := path
					for parent != filepath.Dir(parent) {
						parent = filepath.Dir(parent)
						if parentInfo, parentErr := os.Stat(parent); parentErr == nil && parentInfo.IsDir() {
							if !doctorPathWritable(parentInfo) {
								add("message bases", doctorWarn, fmt.Sprintf("area %q base cannot be created under non-writable directory %s", area.Tag, parent), "Grant the BBS service account write and execute access to the parent directory.")
								issues++
							}
							break
						}
					}
				} else {
					add("message bases", doctorWarn, fmt.Sprintf("cannot access area %q base path %s: %v", area.Tag, path, statErr), "Check the path and filesystem permissions for the BBS process.")
					issues++
				}
			} else if !info.IsDir() {
				add("message bases", doctorWarn, fmt.Sprintf("area %q base path is not a directory: %s", area.Tag, path), "Move or rename the conflicting file and let ViSiON/3 create the message base directory.")
				issues++
			} else if !doctorPathWritable(info) {
				add("message bases", doctorWarn, fmt.Sprintf("area %q base directory is not writable by the account running the doctor", area.Tag), "Grant the BBS service account write and execute access to this directory.")
				issues++
			}
		}
	}
	issues += checkFileEchoAreas(configDir, ftnCfg, conferences, add)
	if issues == 0 {
		add("message areas", doctorOK, "conference references, tags, base paths, and FTN network references are consistent", "")
	}
}

func checkFileEchoAreas(configDir string, ftnCfg config.FTNConfig, conferences map[int]bool, add func(string, doctorSeverity, string, string)) int {
	path := filepath.Join(configDir, "file_areas.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			add("file areas", doctorWarn, "cannot read configs/file_areas.json: "+err.Error(), "Check permissions and restore the file if needed.")
			return 1
		}
		return 0
	}
	var areas []struct {
		ID           int    `json:"id"`
		Tag          string `json:"tag"`
		Path         string `json:"path"`
		ConferenceID int    `json:"conference_id"`
		Network      string `json:"network"`
		FileEcho     string `json:"file_echo"`
	}
	if err := json.Unmarshal(data, &areas); err != nil {
		add("file areas", doctorFail, "configs/file_areas.json: "+jsonErrorLocation(data, err), "Fix configs/file_areas.json at the reported line and column.")
		return 1
	}
	issues, fileEchoCount := 0, 0
	seenTags, seenPaths, seenEchoes := map[string]string{}, map[string]string{}, map[string]string{}
	for _, area := range areas {
		label := area.Tag
		if label == "" {
			label = fmt.Sprintf("ID %d", area.ID)
		}
		if area.ConferenceID != 0 && !conferences[area.ConferenceID] {
			add("file areas", doctorWarn, fmt.Sprintf("file area %q references missing conference %d", label, area.ConferenceID), "Add the conference or change the area's conference_id.")
			issues++
		}
		if area.Tag != "" {
			key := strings.ToLower(strings.TrimSpace(area.Tag))
			if prev, ok := seenTags[key]; ok {
				add("file areas", doctorWarn, fmt.Sprintf("file areas %q and %q share tag %q", prev, label, area.Tag), "Give each file area a unique tag.")
				issues++
			} else {
				seenTags[key] = label
			}
		}
		if area.Path != "" {
			key := strings.ToLower(filepath.Clean(area.Path))
			if prev, ok := seenPaths[key]; ok {
				add("file areas", doctorWarn, fmt.Sprintf("file areas %q and %q share path %q", prev, label, area.Path), "Give each file area its own path under data/files.")
				issues++
			} else {
				seenPaths[key] = label
			}
			clean := filepath.Clean(area.Path)
			if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
				add("file areas", doctorFail, fmt.Sprintf("file area %q has unsafe path %q; the file manager will skip it", label, area.Path), "Use a relative directory contained under data/files, such as uploads or utilities.")
				issues++
			}
		} else {
			add("file areas", doctorFail, fmt.Sprintf("file area %q has no path; the file manager will skip it", label), "Set path to a unique relative directory under data/files.")
			issues++
		}
		configuredNetwork, configuredEcho := strings.TrimSpace(area.Network) != "", strings.TrimSpace(area.FileEcho) != ""
		if configuredNetwork != configuredEcho {
			add("file echo areas", doctorWarn, fmt.Sprintf("file area %q has only one of network/file_echo configured", label), "Set both fields for an FTN file echo, or clear both for a local area.")
			issues++
			continue
		}
		if !configuredNetwork {
			continue
		}
		fileEchoCount++
		network, exists := lookupFTNNetwork(ftnCfg.Networks, area.Network), false
		for key := range ftnCfg.Networks {
			if strings.EqualFold(key, area.Network) {
				exists = true
				break
			}
		}
		if !exists {
			add("file echo areas", doctorWarn, fmt.Sprintf("file area %q references unknown FTN network %q", label, area.Network), "Add that network in configs/ftn.json or correct the file area's network field.")
			issues++
			continue
		}
		echoKey := strings.ToLower(area.Network + "/" + area.FileEcho)
		if prev, ok := seenEchoes[echoKey]; ok {
			add("file echo areas", doctorWarn, fmt.Sprintf("file echo %q on network %q is assigned to both %q and %q", area.FileEcho, area.Network, prev, label), "Assign each network/file echo pair to one file area.")
			issues++
		} else {
			seenEchoes[echoKey] = label
		}
		if len(network.Links) == 0 {
			add("file echo areas", doctorWarn, fmt.Sprintf("file area %q uses FTN network %q, which has no links", label, area.Network), "Add a hub/link under that network in configs/ftn.json.")
			issues++
		} else if strings.TrimSpace(ftnCfg.SecureInboundPath) == "" {
			for _, link := range network.Links {
				if strings.TrimSpace(link.TICPassword) == "" {
					add("file echo areas", doctorWarn, fmt.Sprintf("file area %q receives TIC files from %s without a tic_password, and no secure_inbound_path is configured", label, link.Address), "Set tic_password for this link or configure a secure_inbound_path that only authenticated BinkP sessions can write to.")
					issues++
				}
			}
		}
		clean := filepath.Clean(area.Path)
		if area.Path == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			continue // The unsafe or empty path was reported above.
		}
		if _, statErr := os.Stat(filepath.Join(filepath.Dir(configDir), "data", "files", clean)); errors.Is(statErr, os.ErrNotExist) {
			add("file echo areas", doctorInfo, fmt.Sprintf("file area %q directory will be created on startup; until then, inbound TIC files have nowhere to land", label), "Confirm the BBS process can create data/files/"+filepath.ToSlash(clean)+".")
		} else if statErr != nil {
			add("file echo areas", doctorWarn, fmt.Sprintf("cannot access directory for file area %q: %v", label, statErr), "Check the BBS process's permissions under data/files.")
			issues++
		} else if info, statErr := os.Stat(filepath.Join(filepath.Dir(configDir), "data", "files", clean)); statErr == nil && !info.IsDir() {
			add("file echo areas", doctorWarn, fmt.Sprintf("file area %q path is not a directory", label), "Move the conflicting file and allow the file manager to create its directory.")
			issues++
		}
	}
	if fileEchoCount > 0 && issues == 0 {
		add("file echo areas", doctorOK, fmt.Sprintf("%d FTN file echo area(s) have matching networks and unique routing", fileEchoCount), "")
	}
	return issues
}

func lookupFTNNetwork(networks map[string]config.FTNNetworkConfig, name string) config.FTNNetworkConfig {
	for key, value := range networks {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return config.FTNNetworkConfig{}
}

func lookupKey(networks map[string]config.QWKNetworkConfig, name string) string {
	for key := range networks {
		if strings.EqualFold(key, name) {
			return key
		}
	}
	return name
}

func checkV3Net(configDir, root string, add func(string, doctorSeverity, string, string)) {
	cfg, err := config.LoadV3NetConfig(configDir)
	if err != nil {
		add("V3Net configuration", doctorFail, "configs/v3net.json: "+err.Error(), "Fix configs/v3net.json.")
		return
	}
	if !cfg.Enabled {
		return
	}
	issues := 0
	keyPath := strings.TrimSpace(cfg.KeystorePath)
	if keyPath == "" {
		keyPath = filepath.Join("data", "v3net.key")
	}
	keyPath = resolveFromRoot(root, keyPath)
	keyData, keyErr := os.ReadFile(keyPath)
	if keyErr != nil {
		add("V3Net identity", doctorWarn, fmt.Sprintf("keystore is missing or unreadable at %s: %v", keyPath, keyErr), "Configure or restore the V3Net identity keystore; the doctor will not generate a replacement key.")
		issues++
	} else {
		var key struct {
			Private string `json:"privkey_b64"`
			Public  string `json:"pubkey_b64"`
		}
		parseErr := json.Unmarshal(keyData, &key)
		private, privateErr := base64.StdEncoding.DecodeString(key.Private)
		public, publicErr := base64.StdEncoding.DecodeString(key.Public)
		if parseErr != nil || privateErr != nil || publicErr != nil || len(private) != ed25519.PrivateKeySize || len(public) != ed25519.PublicKeySize || !bytes.Equal(ed25519.PrivateKey(private).Public().(ed25519.PublicKey), ed25519.PublicKey(public)) {
			add("V3Net identity", doctorWarn, "keystore is not a valid Ed25519 identity keypair", "Restore the matching keystore backup; generating a new identity changes this board's V3Net identity.")
			issues++
		} else {
			add("V3Net identity", doctorOK, "Ed25519 keypair is present and structurally valid", "")
			if runtime.GOOS != "windows" {
				if info, statErr := os.Stat(keyPath); statErr == nil && info.Mode().Perm()&0o077 != 0 {
					add("V3Net keystore permissions", doctorWarn, fmt.Sprintf("private key is accessible by group or other users (%04o)", info.Mode().Perm()), "Restrict the keystore to the BBS operator, for example chmod 600 "+keyPath+".")
					issues++
				}
			}
		}
	}
	seen := map[string]bool{}
	for _, network := range cfg.Hub.Networks {
		key := strings.ToLower(strings.TrimSpace(network.Name))
		if key == "" {
			add("V3Net configuration", doctorWarn, "hub network has an empty name", "Give each hub network a name; leaves and areas use this value.")
			issues++
		} else if seen[key] {
			add("V3Net configuration", doctorWarn, fmt.Sprintf("hub network name %q is duplicated", network.Name), "Use a unique name for each V3Net network.")
			issues++
		} else {
			seen[key] = true
		}
	}
	for i, leaf := range cfg.Leaves {
		label := fmt.Sprintf("leaf %d", i+1)
		parsed, parseErr := url.ParseRequestURI(strings.TrimSpace(leaf.HubURL))
		if parseErr != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			add("V3Net configuration", doctorWarn, fmt.Sprintf("%s has invalid hubUrl %q", label, leaf.HubURL), "Use the hub's full http:// or https:// URL.")
			issues++
		}
		if strings.TrimSpace(leaf.Network) == "" {
			add("V3Net configuration", doctorWarn, label+" has no network name", "Set network to the name published by the hub.")
			issues++
		}
		if interval, intervalErr := time.ParseDuration(leaf.PollInterval); intervalErr != nil || interval <= 0 {
			add("V3Net configuration", doctorWarn, fmt.Sprintf("%s has invalid pollInterval %q", label, leaf.PollInterval), "Use a positive duration such as 5m or 1h.")
			issues++
		}
		if len(leaf.Boards) == 0 {
			add("V3Net configuration", doctorWarn, label+" has no boards", "Select at least one local board to receive this network's messages.")
			issues++
		}
	}
	if cfg.Hub.Enabled && len(cfg.Hub.Networks) == 0 {
		add("V3Net configuration", doctorWarn, "hub is enabled but publishes no networks", "Add at least one named network under hub.networks.")
		issues++
	}
	if issues == 0 {
		add("V3Net configuration", doctorOK, fmt.Sprintf("enabled; %d hub network(s), %d leaf subscription(s)", len(cfg.Hub.Networks), len(cfg.Leaves)), "")
	}
}

func checkQWKNet(configDir string, serverCfg config.ServerConfig, serverErr error, root string, add func(string, doctorSeverity, string, string)) {
	cfg, err := config.LoadQWKNetConfig(configDir)
	if err != nil {
		message := jsonFileError(filepath.Join(configDir, "qwknet.json"), &config.QWKNetConfig{}, err)
		message = "configs/qwknet.json: " + message
		add("QWKnet configuration", doctorFail, message, "Fix configs/qwknet.json.")
		return
	}
	if len(cfg.Networks) == 0 {
		return
	}
	systemID := ""
	if serverErr == nil {
		systemID = serverCfg.QWKID
	}
	issues := 0
	for _, key := range cfg.NetworkKeys() {
		if err := config.ValidateQWKNetwork(key, cfg.Networks[key], systemID); err != nil {
			add("QWKnet configuration", doctorWarn, err.Error(), "Correct the QWKnet settings before enabling this network.")
			issues++
		}
	}
	cfg.ResolvePaths(root)
	if err := cfg.ValidateHubIDs(); err != nil {
		add("QWKnet configuration", doctorWarn, err.Error(), "Use a unique hubId for each QWK network.")
		issues++
	}
	if issues == 0 {
		add("QWKnet configuration", doctorOK, fmt.Sprintf("%d network(s) pass existing validation", len(cfg.Networks)), "")
	}
}

func checkUnknownTopLevelKeys(path string, typ reflect.Type, add func(string, doctorSeverity, string, string)) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var value json.RawMessage = data
	unknown := collectUnknownJSONKeys(value, typ, "")
	if len(unknown) == 0 {
		return
	}
	sort.Strings(unknown)
	add(filepath.ToSlash(filepath.Join("configs", filepath.Base(path))), doctorWarn, "unknown key(s): "+strings.Join(unknown, ", "), "Check for misspelled or obsolete setting names.")
}

func collectUnknownJSONKeys(raw json.RawMessage, typ reflect.Type, prefix string) []string {
	if len(raw) == 0 || typ == nil || typ == reflect.TypeOf(json.RawMessage{}) {
		return nil
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return nil
		}
		known := make(map[string]reflect.Type, typ.NumField())
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" {
				name = field.Name
			}
			if name != "-" {
				known[name] = field.Type
			}
		}
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var unknown []string
		for _, key := range keys {
			if key == "_comment" {
				continue
			}
			fieldType, ok := known[key]
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if !ok {
				unknown = append(unknown, path)
				continue
			}
			unknown = append(unknown, collectUnknownJSONKeys(fields[key], fieldType, path)...)
		}
		return unknown
	case reflect.Slice, reflect.Array:
		if typ.Elem().Kind() == reflect.Uint8 {
			return nil
		}
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return nil
		}
		var unknown []string
		for i, value := range values {
			path := fmt.Sprintf("%s[%d]", prefix, i)
			unknown = append(unknown, collectUnknownJSONKeys(value, typ.Elem(), path)...)
		}
		return unknown
	case reflect.Map:
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return nil
		}
		var unknown []string
		for key, value := range values {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			unknown = append(unknown, collectUnknownJSONKeys(value, typ.Elem(), path)...)
		}
		return unknown
	}
	return nil
}

func jsonLocationError(data []byte) string {
	if err := json.Unmarshal(data, new(any)); err != nil {
		return jsonErrorLocation(data, err)
	}
	return "invalid JSON"
}

func jsonFileError(path string, dst any, fallback error) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback.Error()
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return jsonErrorLocation(data, err)
	}
	return fallback.Error()
}

func jsonErrorLocation(data []byte, err error) string {
	var offset int64
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntaxErr):
		offset = syntaxErr.Offset
	case errors.As(err, &typeErr):
		offset = typeErr.Offset
	default:
		return err.Error()
	}
	if offset <= 0 {
		return err.Error()
	}

	// encoding/json offsets count bytes from one. Clamp the reported location
	// to the input so truncated documents still get a useful end-of-file spot.
	index := int(offset - 1)
	if index > len(data) {
		index = len(data)
	}
	line, column := 1, 1
	for remaining := data[:index]; len(remaining) > 0; {
		r, size := utf8.DecodeRune(remaining)
		if r == '\n' {
			line++
			column = 1
		} else {
			column++
		}
		remaining = remaining[size:]
	}
	return fmt.Sprintf("line %d, column %d: %s", line, column, err)
}

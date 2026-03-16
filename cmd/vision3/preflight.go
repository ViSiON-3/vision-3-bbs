package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// preflightResult holds the outcome of a single preflight check.
type preflightResult struct {
	name     string
	ok       bool
	critical bool   // true = blocks startup, false = warning only
	message  string // detail when check fails
}

// runPreflight validates that all required files and directories exist
// before the server attempts to start. It prints a clear summary of what
// is missing and directs the user to run the appropriate setup script.
// Returns true if all critical checks pass.
func runPreflight(basePath string) bool {
	configPath := filepath.Join(basePath, "configs")
	dataPath := filepath.Join(basePath, "data")

	var results []preflightResult

	// Helper to add a check result.
	check := func(name string, critical bool, path string, isDir bool) {
		info, err := os.Stat(path)
		if err != nil {
			msg := "missing"
			if !critical {
				msg = "missing (recommended)"
			}
			results = append(results, preflightResult{name: name, ok: false, critical: critical, message: msg})
			return
		}
		if isDir && !info.IsDir() {
			results = append(results, preflightResult{name: name, ok: false, critical: critical, message: "exists but is not a directory"})
			return
		}
		results = append(results, preflightResult{name: name, ok: true, critical: critical})
	}

	// --- Required directories (critical) ---
	check("configs/", true, configPath, true)
	check("data/users/", true, filepath.Join(dataPath, "users"), true)
	check("data/logs/", true, filepath.Join(dataPath, "logs"), true)
	check("data/msgbases/", true, filepath.Join(dataPath, "msgbases"), true)
	check("data/files/", true, filepath.Join(dataPath, "files"), true)
	check("menus/v3/", true, filepath.Join(basePath, "menus", "v3"), true)

	// --- Required config (critical) ---
	check("configs/config.json", true, filepath.Join(configPath, "config.json"), false)

	// --- Recommended config files (non-critical) ---
	check("configs/strings.json", false, filepath.Join(configPath, "strings.json"), false)
	check("configs/doors.json", false, filepath.Join(configPath, "doors.json"), false)
	check("configs/message_areas.json", false, filepath.Join(configPath, "message_areas.json"), false)
	check("configs/file_areas.json", false, filepath.Join(configPath, "file_areas.json"), false)
	check("configs/protocols.json", false, filepath.Join(configPath, "protocols.json"), false)
	check("data/users/users.json", false, filepath.Join(dataPath, "users", "users.json"), false)

	// --- SSH host key ---
	// Peek at config.json to determine if SSH is enabled; if so the key is critical.
	sshEnabled := isSSHEnabled(filepath.Join(configPath, "config.json"))
	sshKeyPath := filepath.Join(configPath, "ssh_host_rsa_key")
	if _, err := os.Stat(sshKeyPath); err != nil {
		if sshEnabled {
			results = append(results, preflightResult{
				name:     "configs/ssh_host_rsa_key",
				ok:       false,
				critical: true,
				message:  "SSH host key missing (SSH is enabled in config)",
			})
		} else {
			results = append(results, preflightResult{
				name:     "configs/ssh_host_rsa_key",
				ok:       false,
				critical: false,
				message:  "SSH host key missing (not required — SSH is disabled)",
			})
		}
	} else {
		results = append(results, preflightResult{name: "configs/ssh_host_rsa_key", ok: true})
	}

	// --- Evaluate results ---
	criticalFails := 0
	warnFails := 0
	for _, r := range results {
		if r.ok {
			continue
		}
		if r.critical {
			criticalFails++
		} else {
			warnFails++
		}
	}

	if criticalFails == 0 && warnFails == 0 {
		return true
	}

	// Print report
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintln(os.Stderr, "  ViSiON/3 Pre-flight Check")
	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintln(os.Stderr, "")

	for _, r := range results {
		if r.ok {
			fmt.Fprintf(os.Stderr, "  [OK]   %s\n", r.name)
		} else if r.critical {
			fmt.Fprintf(os.Stderr, "  [FAIL] %s — %s\n", r.name, r.message)
		} else {
			fmt.Fprintf(os.Stderr, "  [WARN] %s — %s\n", r.name, r.message)
		}
	}

	fmt.Fprintln(os.Stderr, "")

	if criticalFails > 0 {
		fmt.Fprintf(os.Stderr, "  %d critical issue(s), %d warning(s).\n", criticalFails, warnFails)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  It looks like the initial setup has not been completed.")
		fmt.Fprintln(os.Stderr, "  Please run the setup script for your platform:")
		fmt.Fprintln(os.Stderr, "")
		if runtime.GOOS == "windows" {
			fmt.Fprintln(os.Stderr, "    .\\setup.bat        (Command Prompt)")
			fmt.Fprintln(os.Stderr, "    .\\setup.ps1        (PowerShell)")
		} else {
			fmt.Fprintln(os.Stderr, "    ./setup.sh")
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  For detailed instructions see: https://vision3bbs.com/sysop/")
	} else {
		fmt.Fprintf(os.Stderr, "  %d warning(s) — startup will continue.\n", warnFails)
	}

	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintln(os.Stderr, "")

	// Only block startup on critical failures.
	return criticalFails == 0
}

// isSSHEnabled does a lightweight read of config.json to check if SSH is
// enabled. Returns true by default (SSH is enabled by default in the
// server config) or if config.json cannot be read.
func isSSHEnabled(configFile string) bool {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return true // assume enabled if we can't read config
	}
	var cfg struct {
		SSHEnabled *bool `json:"sshEnabled"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return true
	}
	if cfg.SSHEnabled == nil {
		return true // default is enabled
	}
	return *cfg.SSHEnabled
}

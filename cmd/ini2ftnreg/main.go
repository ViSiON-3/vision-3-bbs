// Command ini2ftnreg converts Synchronet's exec/init-fidonet.ini to the
// embedded JSON registry format used by the FTN Setup Wizard.
//
// This is a build-time tool invoked by build_all.sh before compiling Go
// binaries, ensuring the registry always reflects the latest upstream data.
//
// Usage:
//
//	go run ./cmd/ini2ftnreg -in path/to/init-fidonet.ini -out internal/ftn/registry.json
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Network represents a single FTN network entry in the registry.
type Network struct {
	Zone             int      `json:"zone"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	InfoURL          string   `json:"info_url,omitempty"`
	PackURL          string   `json:"pack_url,omitempty"`
	Coordinator      string   `json:"coordinator,omitempty"`
	CoordinatorEmail string   `json:"coordinator_email,omitempty"`
	CoordinatorFTN   string   `json:"coordinator_ftn,omitempty"`
	AlsoContact      string   `json:"also_contact,omitempty"`
	HubAddress       string   `json:"hub_address,omitempty"`
	HubHostname      string   `json:"hub_hostname,omitempty"`
	HubPort          int      `json:"hub_port,omitempty"`
	DNSSuffix        string   `json:"dns_suffix,omitempty"`
	EcholistURL      string   `json:"echolist_url,omitempty"`
	AreatagPrefix    string   `json:"areatag_prefix,omitempty"`
	AreatagExclude   []string `json:"areatag_exclude,omitempty"`
	AreatitlePrefix  string   `json:"areatitle_prefix,omitempty"`
	HandlesAllowed   bool     `json:"handles_allowed,omitempty"`
	AreaManager      string   `json:"area_manager,omitempty"`
}

func main() {
	inPath := flag.String("in", "", "path to init-fidonet.ini")
	outPath := flag.String("out", "", "path to write registry.json")
	flag.Parse()

	if *inPath == "" || *outPath == "" {
		fmt.Fprintf(os.Stderr, "Usage: ini2ftnreg -in <ini-file> -out <json-file>\n")
		os.Exit(1)
	}

	networks, err := parseINI(*inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", *inPath, err)
		os.Exit(1)
	}

	if len(networks) == 0 {
		fmt.Fprintf(os.Stderr, "Warning: no networks found in %s\n", *inPath)
	}

	data, err := json.MarshalIndent(networks, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}
	data = append(data, '\n')

	if err := os.WriteFile(*outPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", *outPath, err)
		os.Exit(1)
	}

	fmt.Printf("Wrote %d networks to %s\n", len(networks), *outPath)
}

func parseINI(path string) ([]Network, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var networks []Network
	var current *Network

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and blank lines
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}

		// Section header: [zone:N]
		if strings.HasPrefix(line, "[zone:") && strings.HasSuffix(line, "]") {
			zoneStr := line[6 : len(line)-1]
			zone, err := strconv.Atoi(zoneStr)
			if err != nil {
				return nil, fmt.Errorf("invalid zone number %q: %w", zoneStr, err)
			}
			// Save previous network
			if current != nil {
				networks = append(networks, *current)
			}
			current = &Network{Zone: zone}
			continue
		}

		// Key = Value
		if current == nil {
			continue // key outside of section, skip
		}

		eqIdx := strings.Index(line, "=")
		if eqIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eqIdx])
		val := strings.TrimSpace(line[eqIdx+1:])

		switch key {
		case "name":
			current.Name = val
		case "desc":
			current.Description = val
		case "info":
			current.InfoURL = val
		case "pack":
			current.PackURL = val
		case "coord":
			current.Coordinator = val
		case "email":
			current.CoordinatorEmail = val
		case "fido":
			current.CoordinatorFTN = val
		case "also":
			current.AlsoContact = val
		case "addr":
			current.HubAddress = val
		case "host":
			current.HubHostname = val
		case "port":
			port, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("invalid port %q in zone %d: %w", val, current.Zone, err)
			}
			current.HubPort = port
		case "dns":
			current.DNSSuffix = val
		case "echolist":
			current.EcholistURL = val
		case "areatag_prefix":
			current.AreatagPrefix = val
		case "areatag_exclude":
			for _, tag := range strings.Split(val, ",") {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					current.AreatagExclude = append(current.AreatagExclude, tag)
				}
			}
		case "areatitle_prefix":
			current.AreatitlePrefix = val
		case "handles":
			current.HandlesAllowed = strings.EqualFold(val, "true")
		case "areamgr":
			current.AreaManager = val
		}
		// Note: "areatax_prefix" typo in iLink section is intentionally ignored
	}

	// Don't forget the last entry
	if current != nil {
		networks = append(networks, *current)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return networks, nil
}

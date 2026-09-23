package configeditor

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

// sliceToCSV joins a string slice with ", " for display.
func sliceToCSV(s []string) string {
	return strings.Join(s, ", ")
}

// csvToSlice splits a comma-separated string into a trimmed string slice.
// Returns nil for empty input.
func csvToSlice(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// doorCommandsGet returns the command string for display.
// Native doors: "command arg1, arg2, ..." (Commands[0] + Commands[1:])
// DOS doors: "cmd1, cmd2, ..." (each Commands entry is a batch line)
func doorCommandsGet(d *doorEditProxy) string {
	if len(d.Commands) == 0 {
		return ""
	}
	if d.IsDOS {
		return sliceToCSV(d.Commands)
	}
	// Native: first entry is command, rest are args
	if len(d.Commands) == 1 {
		return d.Commands[0]
	}
	return d.Commands[0] + " " + sliceToCSV(d.Commands[1:])
}

// doorCommandsSet parses the command string back into the Commands field.
// Native doors: first token is command, rest are comma-separated args.
// DOS doors: comma-separated list of batch commands.
func doorCommandsSet(d *doorEditProxy, val string) {
	if d.IsDOS {
		d.Commands = csvToSlice(val)
		return
	}
	val = strings.TrimSpace(val)
	if val == "" {
		d.Commands = nil
		return
	}
	// Split first space-separated token as the command
	parts := strings.SplitN(val, " ", 2)
	d.Commands = []string{parts[0]}
	if len(parts) > 1 {
		d.Commands = append(d.Commands, csvToSlice(parts[1])...)
	}
}

// envMapToCSV serializes a map[string]string as "KEY=VALUE, KEY2=VALUE2" for display.
// Keys are sorted for stable output.
func envMapToCSV(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(m))
	for _, k := range keys {
		pairs = append(pairs, k+"="+m[k])
	}
	return strings.Join(pairs, ", ")
}

// csvToEnvMap parses "KEY=VALUE, KEY2=VALUE2" into a map[string]string.
// Returns nil for empty input.
func csvToEnvMap(s string) (map[string]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	result := make(map[string]string, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid entry %q: expected KEY=VALUE", p)
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if key == "" {
			return nil, fmt.Errorf("empty key in %q", p)
		}
		result[key] = val
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// doorEditProxy wraps DoorConfig fields for in-place editing via closures.
type doorEditProxy = config.DoorConfig

// doorTypeLabel returns a short label for the door type, used in list views.
func doorTypeLabel(d *config.DoorConfig) string {
	switch d.Type {
	case "synchronet_js":
		return "SyncJS"
	case "v3_script":
		return "VPL"
	case "rlogin":
		return "RLogin"
	}
	if d.IsDOS {
		return "DOS"
	}
	return "Native"
}

// isSyncJS returns true if the door is a Synchronet JS door.
func isSyncJS(d *doorEditProxy) bool {
	return d.Type == "synchronet_js"
}

// isV3Script returns true if the door is a Vision/3 VPL script.
func isV3Script(d *doorEditProxy) bool {
	return d.Type == "v3_script"
}

// isRLogin returns true if the door is an outbound RLogin door-server link.
func isRLogin(d *doorEditProxy) bool {
	return d.Type == "rlogin"
}

// fieldsDoor returns fields for editing a door program.
// Fields shown depend on door type: native, DOS, or synchronet_js.
func (m *Model) fieldsDoor() []fieldDef {
	keys := m.doorKeys()
	idx := m.recordEditIdx
	if idx < 0 || idx >= len(keys) {
		return nil
	}
	key := keys[idx]
	d := m.configs.Doors[key]
	dPtr := &d

	// Store back closure to update the map entry
	save := func() {
		m.configs.Doors[key] = *dPtr
	}

	row := 1
	fields := []fieldDef{
		{
			// Code is the door's identity: doors.json is saved as an array and
			// re-keyed by Code on load, and DOOR:CODE menu lookups uppercase the
			// input — so renaming re-keys the map and normalizes to uppercase.
			Label: "Code", Help: "Code for DOOR:CODE menu commands (A-Z 0-9 _ - , max 16)", Type: ftString, Col: 3, Row: row, Width: 16,
			Get: func() string { return dPtr.Code },
			Set: func(rawVal string) error {
				val, err := config.NormalizeDoorCode(rawVal)
				if err != nil {
					return err
				}
				for k := range m.configs.Doors {
					if k != key && strings.EqualFold(k, val) {
						return fmt.Errorf("door %q already exists", k)
					}
				}
				cfg := m.configs.Doors[key]
				cfg.Code = val
				m.configs.Doors[val] = cfg
				if val != key {
					delete(m.configs.Doors, key)
				}
				dPtr.Code = val // keep display current until fields are rebuilt
				return nil
			},
			// AfterSet runs on the current model (not the stale captured pointer), so index
			// updates here are correctly applied before buildRecordFields is called.
			AfterSet: func(cur *Model, val string) {
				val = strings.ToUpper(strings.TrimSpace(val))
				newKeys := cur.doorKeys()
				idx := sort.SearchStrings(newKeys, val)
				if idx < len(newKeys) && newKeys[idx] == val {
					cur.recordEditIdx = idx
					cur.recordCursor = idx // keep list selection in sync for when user exits edit
				}
				cur.stayOnField = true // Stay on Code so user can see the updated value
			},
		},
	}

	row++
	fields = append(fields, fieldDef{
		Label: "Name", Help: "Display name shown to users (case preserved)", Type: ftString, Col: 3, Row: row, Width: 30,
		Get: func() string { return dPtr.Name },
		Set: func(val string) error { dPtr.Name = val; save(); return nil },
	})

	// Door type selector — determines which type-specific fields are shown
	row++
	fields = append(fields, fieldDef{
		Label: "Type", Help: "Door type: Native, DOS (dosemu2), Synchronet JS, VPL script, or RLogin", Type: ftLookup, Col: 3, Row: row, Width: 20,
		Get: func() string {
			switch dPtr.Type {
			case "synchronet_js":
				return "synchronet_js"
			case "v3_script":
				return "v3_script"
			case "rlogin":
				return "rlogin"
			}
			if dPtr.IsDOS {
				return "dos"
			}
			return "native"
		},
		Set: func(val string) error {
			switch val {
			case "synchronet_js":
				dPtr.Type = "synchronet_js"
				dPtr.IsDOS = false
			case "v3_script":
				dPtr.Type = "v3_script"
				dPtr.IsDOS = false
			case "rlogin":
				dPtr.Type = "rlogin"
				dPtr.IsDOS = false
			case "dos":
				dPtr.Type = ""
				dPtr.IsDOS = true
			default:
				dPtr.Type = ""
				dPtr.IsDOS = false
			}
			save()
			return nil
		},
		LookupItems: func() []LookupItem {
			return []LookupItem{
				{Value: "native", Display: "Native - Linux/macOS/Windows binary"},
				{Value: "dos", Display: "DOS - DOS door via dosemu2"},
				{Value: "synchronet_js", Display: "Synchronet JS - JavaScript door game"},
				{Value: "v3_script", Display: "VPL Script - Vision/3 JavaScript script"},
				{Value: "rlogin", Display: "RLogin - Outbound link to a door server"},
			}
		},
	})

	row++
	fields = append(fields, fieldDef{
		Label: "Working Dir", Help: "Directory to run the door in", Type: ftString, Col: 3, Row: row, Width: 45,
		Get: func() string { return dPtr.WorkingDirectory },
		Set: func(val string) error { dPtr.WorkingDirectory = val; save(); return nil },
	})

	if isSyncJS(dPtr) {
		// Synchronet JS-specific fields
		row++
		fields = append(fields, fieldDef{
			Label: "Script", Help: "Main JS file to execute (relative to working dir)", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return dPtr.Script },
			Set: func(val string) error { dPtr.Script = val; save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Exec Dir", Help: "Synchronet exec directory (system.exec_dir)", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return dPtr.ExecDir },
			Set: func(val string) error { dPtr.ExecDir = val; save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Library Paths", Help: "Search paths for load()/require(), comma-separated", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return sliceToCSV(dPtr.LibraryPaths) },
			Set: func(val string) error { dPtr.LibraryPaths = csvToSlice(val); save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Script Args", Help: "Arguments passed to script (available as argv), comma-separated", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return sliceToCSV(dPtr.Args) },
			Set: func(val string) error { dPtr.Args = csvToSlice(val); save(); return nil },
		})
	} else if isV3Script(dPtr) {
		// VPL script fields — Script and Args only (no Exec Dir or Library Paths)
		row++
		fields = append(fields, fieldDef{
			Label: "Script", Help: "Main JS file to execute (relative to working dir)", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return dPtr.Script },
			Set: func(val string) error { dPtr.Script = val; save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Script Args", Help: "Arguments passed to script (available as v3.args), comma-separated", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return sliceToCSV(dPtr.Args) },
			Set: func(val string) error { dPtr.Args = csvToSlice(val); save(); return nil },
		})
	} else if isRLogin(dPtr) {
		// Outbound RLogin door-server fields. There is no local program, so
		// nothing here concerns commands, dropfiles or the environment.
		row++
		fields = append(fields, fieldDef{
			Label: "Host", Help: "Door server hostname or IP address", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return dPtr.Host },
			Set: func(val string) error { dPtr.Host = strings.TrimSpace(val); save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Port", Help: "Door server TCP port (0 = 513, the RLogin default)", Type: ftInteger, Col: 3, Row: row, Width: 6, Min: 0, Max: 65535,
			Get: func() string { return strconv.Itoa(dPtr.Port) },
			Set: func(val string) error {
				v, err := strconv.Atoi(strings.TrimSpace(val))
				if err != nil || v < 0 || v > 65535 {
					return fmt.Errorf("port must be 0-65535")
				}
				dPtr.Port = v
				save()
				return nil
			},
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Client User", Help: "RLogin client-user-name; some servers expect a password here (blank = {USERHANDLE})", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return dPtr.ClientUsername },
			Set: func(val string) error { dPtr.ClientUsername = val; save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Server User", Help: "RLogin server-user-name, e.g. [TAG]{USERHANDLE} (blank = {USERHANDLE})", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return dPtr.ServerUsername },
			Set: func(val string) error { dPtr.ServerUsername = val; save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Terminal Type", Help: "RLogin terminal-type; door servers read the door code here, e.g. xtrn=LORD (blank = ANSI/38400)", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return dPtr.TerminalType },
			Set: func(val string) error { dPtr.TerminalType = val; save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Connect Timeout", Help: "Seconds to wait for the door server (0 = 10)", Type: ftInteger, Col: 3, Row: row, Width: 5, Min: 0, Max: 300,
			Get: func() string { return strconv.Itoa(dPtr.ConnectTimeout) },
			Set: func(val string) error {
				v, err := strconv.Atoi(strings.TrimSpace(val))
				if err != nil || v < 0 || v > 300 {
					return fmt.Errorf("timeout must be 0-300 seconds")
				}
				dPtr.ConnectTimeout = v
				save()
				return nil
			},
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Disconnect Key", Help: "Key that hangs up the remote session, e.g. ^] (blank = ^], 'none' = disabled)", Type: ftString, Col: 3, Row: row, Width: 10,
			Get: func() string { return dPtr.DisconnectKey },
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if _, _, err := config.ParseDisconnectKey(val); err != nil {
					return err
				}
				dPtr.DisconnectKey = val
				save()
				return nil
			},
		})
	} else {
		// Native and DOS doors have commands and dropfiles
		row++
		fields = append(fields, fieldDef{
			Label: "Commands", Help: "Native: cmd + comma-sep args. Placeholders: {NODEDIR} {DROPFILE} {NODE} {PORT} — add trailing / if the door needs it, e.g. {NODEDIR}/", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return doorCommandsGet(dPtr) },
			Set: func(val string) error { doorCommandsSet(dPtr, val); save(); return nil },
		})

		row++
		fields = append(fields, fieldDef{
			Label: "Dropfile Type", Help: "Dropfile format", Type: ftLookup, Col: 3, Row: row, Width: 15,
			Get: func() string { return dPtr.DropfileType },
			Set: func(val string) error { dPtr.DropfileType = val; save(); return nil },
			LookupItems: func() []LookupItem {
				return []LookupItem{
					{Value: "", Display: "(none)"},
					{Value: "DOOR.SYS", Display: "DOOR.SYS"},
					{Value: "DOOR32.SYS", Display: "DOOR32.SYS"},
					{Value: "CHAIN.TXT", Display: "CHAIN.TXT"},
					{Value: "DORINFO1.DEF", Display: "DORINFO1.DEF"},
				}
			},
		})

		row++
		fields = append(fields, fieldDef{
			Label: "Dropfile Location", Help: "Where to write dropfile", Type: ftLookup, Col: 3, Row: row, Width: 10,
			Get: func() string { return dPtr.DropfileLocation },
			Set: func(val string) error { dPtr.DropfileLocation = val; save(); return nil },
			LookupItems: func() []LookupItem {
				return []LookupItem{
					{Value: "startup", Display: "startup - Working directory (or '.')"},
					{Value: "node", Display: "node - Per-node temp directory"},
				}
			},
		})

		// Dropfile Case only affects native/Windows doors; DOS doors use a
		// separate dosDropfileName path that ignores it, so hide it for DOS.
		if !dPtr.IsDOS {
			row++
			fields = append(fields, fieldDef{
				Label: "Dropfile Case", Help: "Filename case for the dropfile (native/Windows only)", Type: ftLookup, Col: 3, Row: row, Width: 10,
				Get: func() string {
					if dPtr.DropfileCase == "" {
						return "upper"
					}
					return dPtr.DropfileCase
				},
				Set: func(val string) error {
					if strings.EqualFold(val, "upper") {
						// Preserve byte-identical legacy configs: empty means the default "upper".
						dPtr.DropfileCase = ""
					} else {
						dPtr.DropfileCase = val
					}
					save()
					return nil
				},
				LookupItems: func() []LookupItem {
					return []LookupItem{
						{Value: "upper", Display: "upper - DOOR32.SYS (default)"},
						{Value: "lower", Display: "lower - door32.sys"},
					}
				},
			})
		}
	}

	// Common fields for all door types
	row++
	fields = append(fields, fieldDef{
		Label: "Min Access Level", Help: "Minimum user access level (0=no restriction)", Type: ftInteger, Col: 3, Row: row, Width: 5, Min: 0, Max: 255,
		Get: func() string { return strconv.Itoa(dPtr.MinAccessLevel) },
		Set: func(val string) error {
			v, err := strconv.Atoi(strings.TrimSpace(val))
			if err != nil {
				return fmt.Errorf("access level must be 0-255")
			}
			dPtr.MinAccessLevel = v
			save()
			return nil
		},
	})

	row++
	fields = append(fields, fieldDef{
		Label: "Single Instance", Help: "Only allow one node to run this door at a time", Type: ftYesNo, Col: 3, Row: row, Width: 1,
		Get: func() string { return uitext.BoolToYN(dPtr.SingleInstance) },
		Set: func(val string) error { dPtr.SingleInstance = uitext.YNToBool(val); save(); return nil },
	})

	if !isSyncJS(dPtr) && !isV3Script(dPtr) {
		// Cleanup and env vars for native/DOS doors only
		row++
		fields = append(fields, fieldDef{
			Label: "Cleanup Command", Help: "Command to run after door exits (blank=none)", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string {
				if dPtr.CleanupCommand == "" {
					return ""
				}
				if len(dPtr.CleanupArgs) == 0 {
					return dPtr.CleanupCommand
				}
				return dPtr.CleanupCommand + " " + sliceToCSV(dPtr.CleanupArgs)
			},
			Set: func(val string) error {
				val = strings.TrimSpace(val)
				if val == "" {
					dPtr.CleanupCommand = ""
					dPtr.CleanupArgs = nil
					save()
					return nil
				}
				parts := strings.SplitN(val, " ", 2)
				dPtr.CleanupCommand = parts[0]
				if len(parts) > 1 {
					dPtr.CleanupArgs = csvToSlice(parts[1])
				} else {
					dPtr.CleanupArgs = nil
				}
				save()
				return nil
			},
		})

		// Environment variables need a local process to apply to, so they are
		// not offered for a door that is only a socket to somewhere else.
		if !isRLogin(dPtr) {
			row++
			fields = append(fields, fieldDef{
				Label: "Env Vars", Help: "Environment variables: KEY=VALUE, KEY2=VALUE2", Type: ftString, Col: 3, Row: row, Width: 45,
				Get: func() string { return envMapToCSV(dPtr.EnvironmentVars) },
				Set: func(val string) error {
					m, err := csvToEnvMap(val)
					if err != nil {
						return err
					}
					dPtr.EnvironmentVars = m
					save()
					return nil
				},
			})
		}
	}

	if dPtr.IsDOS && !isSyncJS(dPtr) && !isV3Script(dPtr) {
		// DOS-specific fields
		row++
		fields = append(fields, fieldDef{
			Label: "Drive C Path", Help: "Drive C directory path (blank=~/.dosemu/drive_c)", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return dPtr.DriveCPath },
			Set: func(val string) error { dPtr.DriveCPath = val; save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "DOS Emulator", Help: "DOS emulator to use", Type: ftLookup, Col: 3, Row: row, Width: 10,
			Get: func() string { return dPtr.DOSEmulator },
			Set: func(val string) error { dPtr.DOSEmulator = val; save(); return nil },
			LookupItems: func() []LookupItem {
				return []LookupItem{
					{Value: "auto", Display: "auto - Detect available emulator"},
					{Value: "dosemu", Display: "dosemu - dosemu2 (Linux only)"},
				}
			},
		})
		row++
		fields = append(fields, fieldDef{
			Label: "FOSSIL Driver", Help: "DOS FOSSIL driver command (e.g. C:\\UTILS\\X00.EXE eliminate)", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return dPtr.FossilDriver },
			Set: func(val string) error { dPtr.FossilDriver = val; save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "DOSemu Config", Help: "Custom .dosemurc config file (optional)", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return dPtr.DosemuConfig },
			Set: func(val string) error { dPtr.DosemuConfig = val; save(); return nil },
		})
	} else if !dPtr.IsDOS && !isSyncJS(dPtr) && !isV3Script(dPtr) && !isRLogin(dPtr) {
		// Native-specific fields
		row++
		fields = append(fields, fieldDef{
			Label: "I/O Mode", Help: "I/O handling mode", Type: ftLookup, Col: 3, Row: row, Width: 15,
			Get: func() string { return dPtr.IOMode },
			Set: func(val string) error { dPtr.IOMode = val; save(); return nil },
			LookupItems: func() []LookupItem {
				return []LookupItem{
					{Value: "STDIO", Display: "STDIO - Standard I/O redirection"},
					{Value: "SOCKET", Display: "SOCKET - Pass socket FD to door"},
				}
			},
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Raw Terminal", Help: "Allocate PTY for raw terminal I/O", Type: ftYesNo, Col: 3, Row: row, Width: 1,
			Get: func() string { return uitext.BoolToYN(dPtr.RequiresRawTerminal) },
			Set: func(val string) error { dPtr.RequiresRawTerminal = uitext.YNToBool(val); save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Use Shell", Help: "Wrap command in /bin/sh -c (Linux) or cmd /c (Windows)", Type: ftYesNo, Col: 3, Row: row, Width: 1,
			Get: func() string { return uitext.BoolToYN(dPtr.UseShell) },
			Set: func(val string) error { dPtr.UseShell = uitext.YNToBool(val); save(); return nil },
		})
	}

	return fields
}

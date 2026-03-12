package configeditor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stlalpha/vision3/internal/config"
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
// Native doors: "command arg1, arg2, ..."
// DOS doors: "cmd1, cmd2, ..." from DOSCommands
func doorCommandsGet(d *doorEditProxy) string {
	if d.IsDOS {
		return sliceToCSV(d.DOSCommands)
	}
	if d.Command == "" {
		return ""
	}
	if len(d.Args) == 0 {
		return d.Command
	}
	return d.Command + " " + sliceToCSV(d.Args)
}

// doorCommandsSet parses the command string back into the appropriate fields.
// Native doors: first token is command, rest are comma-separated args.
// DOS doors: comma-separated list goes into DOSCommands.
func doorCommandsSet(d *doorEditProxy, val string) {
	if d.IsDOS {
		d.DOSCommands = csvToSlice(val)
		d.Command = ""
		d.Args = nil
		return
	}
	val = strings.TrimSpace(val)
	if val == "" {
		d.Command = ""
		d.Args = nil
		return
	}
	// Split first space-separated token as the command
	parts := strings.SplitN(val, " ", 2)
	d.Command = parts[0]
	if len(parts) > 1 {
		d.Args = csvToSlice(parts[1])
	} else {
		d.Args = nil
	}
}

// doorEditProxy wraps DoorConfig fields for in-place editing via closures.
type doorEditProxy = config.DoorConfig

// fieldsDoor returns fields for editing a door program.
// Fields shown depend on whether the door is DOS or native.
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
			Label: "Name", Help: "Door name used in DOOR:NAME menu commands", Type: ftString, Col: 3, Row: row, Width: 30,
			Get: func() string { return dPtr.Name },
			Set: func(val string) error { dPtr.Name = val; save(); return nil },
		},
	}

	row++
	fields = append(fields, fieldDef{
		Label: "Is DOS", Help: "Y=DOS door (dosemu2), N=native program", Type: ftYesNo, Col: 3, Row: row, Width: 1,
		Get: func() string { return boolToYN(dPtr.IsDOS) },
		Set: func(val string) error { dPtr.IsDOS = ynToBool(val); save(); return nil },
	})

	row++
	fields = append(fields, fieldDef{
		Label: "Commands", Help: "Native: command args / DOS: comma-separated DOS commands", Type: ftString, Col: 3, Row: row, Width: 45,
		Get: func() string { return doorCommandsGet(dPtr) },
		Set: func(val string) error { doorCommandsSet(dPtr, val); save(); return nil },
	})

	row++
	fields = append(fields, fieldDef{
		Label: "Dropfile Type", Help: "DOOR.SYS, DOOR32.SYS, CHAIN.TXT, DORINFO1.DEF, or NONE", Type: ftString, Col: 3, Row: row, Width: 15,
		Get: func() string { return dPtr.DropfileType },
		Set: func(val string) error { dPtr.DropfileType = val; save(); return nil },
	})

	row++
	fields = append(fields, fieldDef{
		Label: "Dropfile Location", Help: "Where to write dropfile: startup (working dir) or node (per-node temp dir)", Type: ftString, Col: 3, Row: row, Width: 10,
		Get: func() string { return dPtr.DropfileLocation },
		Set: func(val string) error {
			val = strings.ToLower(strings.TrimSpace(val))
			switch val {
			case "", "startup", "node":
				dPtr.DropfileLocation = val
				save()
				return nil
			default:
				return fmt.Errorf("invalid location %q: must be blank, startup, or node", val)
			}
		},
	})

	// Common fields for all door types
	row++
	fields = append(fields, fieldDef{
		Label: "Min Access Level", Help: "Minimum user access level (0=no restriction)", Type: ftString, Col: 3, Row: row, Width: 5,
		Get: func() string { return strconv.Itoa(dPtr.MinAccessLevel) },
		Set: func(val string) error {
			v, err := strconv.Atoi(strings.TrimSpace(val))
			if err != nil || v < 0 || v > 255 {
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
		Get: func() string { return boolToYN(dPtr.SingleInstance) },
		Set: func(val string) error { dPtr.SingleInstance = ynToBool(val); save(); return nil },
	})

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

	if dPtr.IsDOS {
		// DOS-specific fields
		row++
		fields = append(fields, fieldDef{
			Label: "Drive C Path", Help: "Drive C directory path (blank=~/.dosemu/drive_c)", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return dPtr.DriveCPath },
			Set: func(val string) error { dPtr.DriveCPath = val; save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "DOS Emulator", Help: "Emulator: blank=auto, dosemu=dosemu2 (Linux x86 only)", Type: ftString, Col: 3, Row: row, Width: 10,
			Get: func() string { return dPtr.DOSEmulator },
			Set: func(val string) error {
				val = strings.ToLower(strings.TrimSpace(val))
				switch val {
				case "", "auto", "dosemu":
					dPtr.DOSEmulator = val
					save()
					return nil
				default:
					return fmt.Errorf("invalid emulator %q: must be blank, auto, or dosemu", val)
				}
			},
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Dropfile Dest", Help: "DOS path to auto-copy dropfile before running (e.g. C:\\DOORS\\LORD)", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return dPtr.DropfileDest },
			Set: func(val string) error { dPtr.DropfileDest = val; save(); return nil },
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
	} else {
		// Native-specific fields
		row++
		fields = append(fields, fieldDef{
			Label: "Working Dir", Help: "Directory to run the command in", Type: ftString, Col: 3, Row: row, Width: 45,
			Get: func() string { return dPtr.WorkingDirectory },
			Set: func(val string) error { dPtr.WorkingDirectory = val; save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "I/O Mode", Help: "I/O handling: STDIO or SOCKET (pass FD to door)", Type: ftString, Col: 3, Row: row, Width: 15,
			Get: func() string { return dPtr.IOMode },
			Set: func(val string) error { dPtr.IOMode = val; save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Raw Terminal", Help: "Allocate PTY for raw terminal I/O", Type: ftYesNo, Col: 3, Row: row, Width: 1,
			Get: func() string { return boolToYN(dPtr.RequiresRawTerminal) },
			Set: func(val string) error { dPtr.RequiresRawTerminal = ynToBool(val); save(); return nil },
		})
		row++
		fields = append(fields, fieldDef{
			Label: "Use Shell", Help: "Wrap command in /bin/sh -c (Linux) or cmd /c (Windows)", Type: ftYesNo, Col: 3, Row: row, Width: 1,
			Get: func() string { return boolToYN(dPtr.UseShell) },
			Set: func(val string) error { dPtr.UseShell = ynToBool(val); save(); return nil },
		})
	}

	return fields
}

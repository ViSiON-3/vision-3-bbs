package main

import "flag"

// cliFlags holds parsed command-line options for wfc.
type cliFlags struct {
	// connect is the SSH target in ssh://user@host:port form.
	connect string
	// readonly prevents the operator from issuing admin commands.
	readonly bool
	// ascii disables box-drawing characters.
	ascii bool
	// noColor disables terminal color output.
	noColor bool
	// refresh is the poll interval in milliseconds (reserved; not yet wired).
	refresh int
	// maxEvents caps the in-memory event ring buffer.
	maxEvents int
	// identity is the path to the SSH private key file.
	identity string
	// knownHosts is the path to the known_hosts file.
	knownHosts string
	// insecure disables SSH host-key verification. Development only.
	insecure bool
	// version prints the build version and exits.
	version bool
}

// registerFlags registers all wfc CLI flags onto fs and returns a pointer to
// the populated cliFlags struct. Call fs.Parse(args) after this returns.
func registerFlags(fs *flag.FlagSet) *cliFlags {
	f := &cliFlags{}
	fs.StringVar(&f.connect, "connect", "", "SSH target: ssh://user@host:port (required)")
	fs.BoolVar(&f.readonly, "readonly", false, "Disable admin commands (view only)")
	fs.BoolVar(&f.ascii, "ascii", false, "Use ASCII-only characters (no box-drawing)")
	fs.BoolVar(&f.noColor, "no-color", false, "Disable color output")
	fs.IntVar(&f.refresh, "refresh", 1000, "Snapshot poll interval in milliseconds (reserved)")
	fs.IntVar(&f.maxEvents, "max-events", 200, "Maximum events to keep in the event log")
	fs.StringVar(&f.identity, "identity", "", "SSH private key file (default: ~/.ssh/id_ed25519)")
	fs.StringVar(&f.knownHosts, "known-hosts", "", "SSH known_hosts file (default: ~/.ssh/known_hosts)")
	fs.BoolVar(&f.insecure, "insecure", false, "Skip SSH host-key verification (development only)")
	fs.BoolVar(&f.version, "version", false, "Print version and exit")
	return f
}

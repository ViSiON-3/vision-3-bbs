// Command wfc is the remote sysop console for ViSiON/3 BBS.
// It connects to a running BBS instance via SSH and launches the WFC
// (Waiting For Call) TUI in the caller's terminal.
//
// Usage:
//
//	wfc --connect ssh://sysop@bbs.example.com:6023 [flags]
package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	gossh "golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
	"github.com/ViSiON-3/vision-3-bbs/internal/wfcui"
)

// version is set at link time via -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	fs := flag.NewFlagSet("wfc", flag.ExitOnError)
	f := registerFlags(fs)

	if err := fs.Parse(os.Args[1:]); err != nil {
		// flag.ExitOnError handles the exit; this is unreachable.
		fmt.Fprintf(os.Stderr, "wfc: %v\n", err)
		os.Exit(2)
	}

	if f.version {
		fmt.Printf("wfc %s\n", version)
		os.Exit(0)
	}

	if f.connect == "" {
		fmt.Fprintln(os.Stderr, "wfc: --connect is required (e.g. ssh://sysop@bbs.example.com:6023)")
		fs.Usage()
		os.Exit(2)
	}

	sshUser, sshAddr, err := parseConnect(f.connect)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wfc: --connect: %v\n", err)
		os.Exit(2)
	}

	identityPath := f.identity
	if identityPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "wfc: resolve home directory: %v\n", err)
			os.Exit(1)
		}
		identityPath = filepath.Join(home, ".ssh", "id_ed25519")
	}

	knownHostsPath := f.knownHosts
	if knownHostsPath == "" && !f.insecure {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "wfc: resolve home directory: %v\n", err)
			os.Exit(1)
		}
		knownHostsPath = filepath.Join(home, ".ssh", "known_hosts")
	}

	signer, err := loadSigner(identityPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wfc: load identity %q: %v\n", identityPath, err)
		os.Exit(1)
	}

	client, err := admin.DialSSH(admin.SSHDialConfig{
		Addr:           sshAddr,
		User:           sshUser,
		Signer:         signer,
		KnownHostsPath: knownHostsPath,
		Insecure:       f.insecure,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "wfc: connect: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	model := wfcui.New(client, wfcui.Options{
		ASCII:     f.ascii,
		NoColor:   f.noColor,
		ReadOnly:  f.readonly,
		MaxEvents: f.maxEvents,
	})

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "wfc: TUI error: %v\n", err)
		os.Exit(1)
	}
}

// parseConnect parses a connect string of the form ssh://user@host:port.
// It returns the username and "host:port" address, or an error if the string
// is not a valid SSH URL with all required components.
func parseConnect(s string) (user, addr string, err error) {
	if s == "" {
		return "", "", fmt.Errorf("connect string is empty")
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "ssh" {
		return "", "", fmt.Errorf("unsupported scheme %q: must be ssh://", u.Scheme)
	}

	if u.User == nil || u.User.Username() == "" {
		return "", "", fmt.Errorf("missing username: use ssh://user@host:port")
	}

	host := u.Hostname()
	if host == "" {
		return "", "", fmt.Errorf("missing host: use ssh://user@host:port")
	}

	port := u.Port()
	if port == "" {
		return "", "", fmt.Errorf("missing port: use ssh://user@host:port")
	}

	return u.User.Username(), host + ":" + port, nil
}

// loadSigner reads the PEM-encoded private key at path and returns an
// ssh.Signer for public-key authentication.
func loadSigner(path string) (gossh.Signer, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	signer, err := gossh.ParsePrivateKey(pem)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return signer, nil
}

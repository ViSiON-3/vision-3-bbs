package bbsregression

import "testing"

func TestNewServerRejectsInvalidProfiles(t *testing.T) {
	valid := Profile{Host: "127.0.0.1", Port: 2323}
	cases := []struct {
		name    string
		profile Profile
	}{
		{"missing_host", Profile{Port: 2323}},
		{"zero_port", Profile{Host: "127.0.0.1"}},
		{"port_out_of_range", Profile{Host: "127.0.0.1", Port: 65536}},
		{"unsupported_protocol", func() Profile { p := valid; p.Protocol = "websocket"; return p }()},
		{"ssh_missing_user", func() Profile { p := valid; p.Protocol = "ssh"; p.SSHHostKeySHA256 = "SHA256:host"; return p }()},
		{"ssh_missing_fingerprint", func() Profile { p := valid; p.Protocol = "ssh"; p.SSHUser = "bbs"; return p }()},
		{"short_terminal", func() Profile { p := valid; p.Columns = 19; return p }()},
		{"tall_terminal", func() Profile { p := valid; p.Rows = 101; return p }()},
		{"unknown_encoding", func() Profile { p := valid; p.Encoding = "latin1"; return p }()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewServer(tc.profile); err == nil {
				t.Fatal("NewServer accepted invalid profile")
			}
		})
	}
}

func TestNewServerAcceptsDefaultTelnetProfile(t *testing.T) {
	if _, err := NewServer(Profile{Host: "127.0.0.1", Port: 2323}); err != nil {
		t.Fatalf("NewServer with default Telnet settings: %v", err)
	}
}

func TestNewServerRequiresPinnedSSHHostKey(t *testing.T) {
	_, err := NewServer(Profile{
		Host: "bbs.example", Port: 2222, Protocol: "ssh", SSHUser: "bbs",
		SSHHostKeySHA256: "SHA256:known-host-key",
	})
	if err != nil {
		t.Fatalf("valid SSH profile: %v", err)
	}
}

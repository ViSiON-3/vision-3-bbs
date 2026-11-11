package v3net

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// rosterPathFor is where the subscription to network on hubURL keeps its
// board roster: beside the node key, one file per subscription, named by
// the network and a short hash of the hub's URL so two hubs for one
// network keep theirs apart.
func (s *Service) rosterPathFor(network, hubURL string) string {
	if s.cfg.KeystorePath == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(hubURL))
	name := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, network)
	return filepath.Join(filepath.Dir(s.cfg.KeystorePath), "v3net_rosters", name+"-"+hex.EncodeToString(sum[:4])+".json")
}

// CentralRoster returns the board roster network's subscription holds, or
// nil when it holds none. It returns an error without a subscription.
func (s *Service) CentralRoster(network string) (*protocol.CentralRoster, error) {
	run, err := s.leafFor(network)
	if err != nil {
		return nil, err
	}
	return run.Roster(), nil
}

// PublishCentralRoster signs members as network's roster, this board being
// its coordinator, and posts it to the hub. base is the seq of the roster
// the edit started from, 0 for none; a roster changed since then is not
// replaced (leaf.ErrRosterChanged).
func (s *Service) PublishCentralRoster(ctx context.Context, network string, base int64, members []protocol.CentralRosterMember) (*protocol.CentralRoster, error) {
	run, err := s.leafFor(network)
	if err != nil {
		return nil, err
	}
	r, err := run.PublishRoster(ctx, base, members)
	if err != nil {
		return nil, fmt.Errorf("publish roster for %s: %w", network, err)
	}
	return r, nil
}

// CentralRosterCandidates returns every verified listing in network's
// directory, on the roster or not, for the coordinator to admit from.
func (s *Service) CentralRosterCandidates(ctx context.Context, network string) ([]protocol.CentralDirectoryEntry, error) {
	run, err := s.leafFor(network)
	if err != nil {
		return nil, err
	}
	return run.FetchCentralDirectoryUnfiltered(ctx, true)
}

// NodePubKeyB64 returns this board's node public key, standard base64.
func (s *Service) NodePubKeyB64() string {
	if s.ks == nil {
		return ""
	}
	return s.ks.PubKeyBase64()
}

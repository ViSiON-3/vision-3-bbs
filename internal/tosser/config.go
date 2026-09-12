package tosser

import "github.com/ViSiON-3/vision-3-bbs/internal/config"

// Type aliases for config types used throughout the tosser package.
type networkConfig = config.FTNNetworkConfig
type linkConfig = config.FTNLinkConfig

// pathConfig holds the FTN settings from FTNConfig that the tosser needs. Most
// are global (shared across all networks); BinkdOutboundPath is resolved per
// network, see below.
type pathConfig struct {
	InboundPath       string
	SecureInboundPath string
	OutboundPath      string

	// BinkdOutboundPath is this network's BSO outbound, already resolved from
	// its own binkd_outbound_path or the global one (see New). Per-network
	// rather than global because BSO flow files are named from the
	// destination net/node with no zone component: two networks sharing one
	// directory and a net/node pair would collide on a single filename, and
	// one network's mail would be handed to the other's hub.
	//
	// OutboundPath (the staging queue) stays shared: staged packets carry
	// their destination zone in the header, and PackOutbound matches on
	// zone:net/node, so each network's tosser takes only its own.
	BinkdOutboundPath string

	TempPath    string
	BadAreaTag  string
	DupeAreaTag string
}

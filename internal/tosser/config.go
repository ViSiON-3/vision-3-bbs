package tosser

import "github.com/ViSiON-3/vision-3-bbs/internal/config"

// Type aliases for config types used throughout the tosser package.
type networkConfig = config.FTNNetworkConfig
type linkConfig = config.FTNLinkConfig

// pathConfig holds the global FTN settings from FTNConfig (shared across all networks).
type pathConfig struct {
	InboundPath       string
	SecureInboundPath string
	OutboundPath      string
	BinkdOutboundPath string
	TempPath          string
	BadAreaTag        string
	DupeAreaTag       string
}

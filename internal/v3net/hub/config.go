// Package hub implements the V3Net hub HTTP server.
package hub

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

// Config holds hub server configuration.
type Config struct {
	ListenAddr string
	DataDir    string
	Keystore   *keystore.Keystore
	// AutoApprove activates new subscribers on registration.
	AutoApprove bool
	// RequireSignedSubscribe opts into rejecting legacy unsigned subscriptions.
	// False preserves compatibility while leaves upgrade independently.
	RequireSignedSubscribe bool
	// AutoApproveAreas adds area proposals to the NAL on arrival instead of
	// queueing them for the coordinator.
	AutoApproveAreas bool
	Networks         []NetworkConfig
}

// NetworkConfig defines a single network hosted by this hub.
type NetworkConfig struct {
	Name        string
	Description string
}

package configeditor

import (
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

// sysFieldsQWKAPI returns fields for the QWK Mobile API sub-screen.
func sysFieldsQWKAPI(cfg *config.ServerConfig) []fieldDef {
	api := &cfg.QWKAPI
	return []fieldDef{
		{
			Label: "Enabled", Help: "Enable the QWK mobile app HTTP API", Type: ftYesNo, Col: 3, Row: 1, Width: 1,
			Get: func() string { return uitext.BoolToYN(api.Enabled) },
			Set: func(val string) error { api.Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Host", Help: "Listen address (blank=all interfaces)", Type: ftString, Col: 3, Row: 2, Width: 20,
			Get: func() string { return api.Host },
			Set: func(val string) error { api.Host = val; return nil },
		},
		{
			Label: "Port", Help: "API listen port (default: 8666)", Type: ftInteger, Col: 3, Row: 3, Width: 5, Min: 1, Max: 65535,
			Get: func() string {
				p := api.Port
				if p == 0 {
					p = 8666
				}
				return strconv.Itoa(p)
			},
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				api.Port = n
				return nil
			},
		},
		{
			Label: "Cert File", Help: "TLS certificate path (blank=auto self-signed)", Type: ftString, Col: 3, Row: 4, Width: 45,
			Get: func() string { return api.CertFile },
			Set: func(val string) error { api.CertFile = val; return nil },
		},
		{
			Label: "Key File", Help: "TLS private key path (blank=auto self-signed)", Type: ftString, Col: 3, Row: 5, Width: 45,
			Get: func() string { return api.KeyFile },
			Set: func(val string) error { api.KeyFile = val; return nil },
		},
		{
			Label: "Token TTL Hrs", Help: "Bearer token lifetime in hours (default: 24)", Type: ftInteger, Col: 3, Row: 6, Width: 5, Min: 1, Max: 8760,
			Get: func() string {
				// Match QWKAPIConfig.TokenTTL, which defaults every
				// non-positive value to 24h.
				h := api.TokenTTLHours
				if h <= 0 {
					h = 24
				}
				return strconv.Itoa(h)
			},
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				api.TokenTTLHours = n
				return nil
			},
		},
	}
}

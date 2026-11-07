package menu

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// QWKWriteAuthorizer returns a per-area write-ACS gate for use outside a
// terminal session (the QWK packet API). ACS is evaluated headlessly: with no
// ssh.Session, a string with a session term (L, A) in it fails as a whole,
// whatever its other parts say; vision3 doctor warns about such strings.
// Empty ACSWrite allows the area, matching the terminal path.
func QWKWriteAuthorizer(u *user.User) func(area *message.MessageArea) bool {
	return func(area *message.MessageArea) bool {
		if area == nil {
			return false
		}
		return area.ACSWrite == "" || checkACS(area.ACSWrite, u, nil, nil, time.Now())
	}
}

// ResolveQWKID exposes the package's QWK BBS-ID resolution for callers outside
// the terminal flow (e.g. the packet API).
func ResolveQWKID(cfg config.ServerConfig) string {
	return resolveQWKID(cfg)
}

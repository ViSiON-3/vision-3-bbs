package user

import (
	"os"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
)

// writeFileAtomic writes data to path by way of a temp file in the same
// directory, then renames it into place.
//
// The mechanics live in internal/atomicfile, which several packages had each
// grown their own copy of. Sharing them matters beyond tidiness: the Windows
// half of the problem is that a rename fails while any reader holds the
// destination open, and a fix in five separate copies is a fix in four of them
// sooner or later.
//
// ./ue has written this way since it was built (internal/usereditor/fileio.go);
// this brings the BBS into line with it.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	return atomicfile.WriteFile(path, data, perm)
}

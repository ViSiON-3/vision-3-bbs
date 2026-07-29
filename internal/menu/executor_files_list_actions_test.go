package menu

import (
	"testing"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestToggleFileTag(t *testing.T) {
	id := uuid.New()
	u := &user.User{}
	st := &fileListState{
		currentUser:  u,
		filesPerPage: 10,
		currentPage:  1,
		filesOnPage:  []file.FileRecord{{ID: id, Filename: "TEST.ZIP"}},
	}
	st.toggleFileTag("1")
	if len(u.TaggedFileIDs) != 1 || u.TaggedFileIDs[0] != id {
		t.Fatalf("tag: got %v, want [%s]", u.TaggedFileIDs, id)
	}
	st.toggleFileTag("1")
	if len(u.TaggedFileIDs) != 0 {
		t.Fatalf("untag: got %v, want empty", u.TaggedFileIDs)
	}
}

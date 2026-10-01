package user

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWFCReadOnlyOmittedWhenOff(t *testing.T) {
	data, err := json.Marshal(&User{Handle: "boss"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "wfcReadOnly") {
		t.Fatalf("an unset flag reached users.json: %s", data)
	}
	var u User
	if err := json.Unmarshal([]byte(`{"handle":"boss","wfcReadOnly":true}`), &u); err != nil || !u.WFCReadOnly {
		t.Fatalf("wfcReadOnly not read back: %+v %v", u, err)
	}
}

// ./ue sets the flag while the BBS runs; the next BBS save must keep it.
func TestExternalWFCReadOnlyEditSurvivesSave(t *testing.T) {
	um, path := mgrWithUser(t, &User{ID: 1, Handle: "Boss", AccessLevel: 255})
	writeUsersFile(t, path, &User{ID: 1, Handle: "Boss", AccessLevel: 255, WFCReadOnly: true})
	if err := um.SaveUsers(); err != nil {
		t.Fatal(err)
	}
	if !readUsersFile(t, path)["boss"].WFCReadOnly {
		t.Fatal("the sysop's read-only setting was overwritten")
	}
	if u, _ := um.GetUser("boss"); !u.WFCReadOnly {
		t.Fatal("the running BBS did not take the read-only setting")
	}
}

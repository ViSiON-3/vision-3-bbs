package user

import "testing"

// The test-account flag is the sysop's: set or cleared in ./ue while the
// BBS runs, it survives the BBS writing back a session copy that carries
// the old value, in both directions.
func TestTestAccountFlagIsSysopOwned(t *testing.T) {
	for _, set := range []bool{true, false} {
		um, path := mgrWithUser(t, &User{ID: 20, Handle: "Robot", AccessLevel: 10, Validated: true, TestAccount: !set})
		sessionCopy, ok := um.GetUser("Robot")
		if !ok {
			t.Fatal("GetUser failed")
		}
		writeUsersFile(t, path, &User{ID: 20, Handle: "Robot", AccessLevel: 10, Validated: true, TestAccount: set})

		sessionCopy.SeenNewsIDs = []int{1}
		if err := um.UpdateUser(sessionCopy); err != nil {
			t.Fatalf("UpdateUser: %v", err)
		}
		if got := readUsersFile(t, path)["robot"].TestAccount; got != set {
			t.Errorf("sysop set TestAccount=%v in ./ue; after the session's save it is %v", set, got)
		}
	}
}

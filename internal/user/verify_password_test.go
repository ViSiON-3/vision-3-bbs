package user

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// VerifyPassword checks a password like Authenticate does, but records no
// call: TimesCalled and LastLogin stay as they were.
func TestVerifyPasswordRecordsNoCall(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	um := NewUserMgrForTest(
		&User{ID: 1, Handle: "Caller", PasswordHash: string(hash)},
		&User{ID: 2, Handle: "Gone", PasswordHash: string(hash), DeletedUser: true},
	)

	if !um.VerifyPassword("caller", "secret") {
		t.Error("the right password (handle in another case) didn't verify")
	}
	if um.VerifyPassword("Caller", "wrong") {
		t.Error("a wrong password verified")
	}
	if um.VerifyPassword("Gone", "secret") {
		t.Error("a deleted account's password verified")
	}
	if um.VerifyPassword("Nobody", "secret") {
		t.Error("an unknown handle verified")
	}
	if u, _ := um.GetUser("Caller"); u.TimesCalled != 0 || !u.LastLogin.IsZero() {
		t.Errorf("VerifyPassword recorded a call: TimesCalled %d, LastLogin %v", u.TimesCalled, u.LastLogin)
	}
}

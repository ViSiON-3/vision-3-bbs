package user

import (
	"reflect"
	"testing"
	"time"
)

// isReference reports whether a value of type t can share memory with a
// copy of it: a map, slice or pointer, or a struct or array holding one.
// time.Time's location pointer is shared on purpose and never written.
func isReference(t reflect.Type) bool {
	if t == reflect.TypeOf(time.Time{}) {
		return false
	}
	switch t.Kind() {
	case reflect.Map, reflect.Slice, reflect.Pointer, reflect.Interface, reflect.Chan, reflect.Func:
		return true
	case reflect.Array:
		return isReference(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if isReference(t.Field(i).Type) {
				return true
			}
		}
	}
	return false
}

// filled is a non-nil value of the reference type t with one element.
func filled(t reflect.Type) reflect.Value {
	switch t.Kind() {
	case reflect.Map:
		m := reflect.MakeMap(t)
		m.SetMapIndex(reflect.New(t.Key()).Elem(), reflect.New(t.Elem()).Elem())
		return m
	case reflect.Slice:
		return reflect.MakeSlice(t, 1, 1)
	}
	return reflect.New(t.Elem())
}

// Every map, slice and pointer on User is copied by Clone, so a field added
// later can't be missed: this fails until Clone copies it. Elements of the
// copied fields must themselves be plain values, since Clone copies one
// level deep.
func TestCloneCopiesEveryReference(t *testing.T) {
	u := &User{}
	v := reflect.ValueOf(u).Elem()
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !isReference(f.Type) {
			continue
		}
		if !f.IsExported() {
			t.Errorf("unexported field %s holds a reference; Clone can't be checked for it", f.Name)
			continue
		}
		switch f.Type.Kind() {
		case reflect.Map, reflect.Slice, reflect.Pointer:
		default:
			t.Errorf("field %s is a %s; Clone copies only maps, slices and pointers", f.Name, f.Type.Kind())
			continue
		}
		if f.Type.Kind() == reflect.Map && isReference(f.Type.Key()) || isReference(f.Type.Elem()) {
			t.Errorf("field %s's elements hold references, which Clone doesn't copy", f.Name)
		}
		v.Field(i).Set(filled(f.Type))
	}

	c := reflect.ValueOf(u.Clone()).Elem()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() || !isReference(f.Type) {
			continue
		}
		orig, cp := v.Field(i), c.Field(i)
		if cp.IsNil() {
			t.Errorf("Clone dropped %s", f.Name)
			continue
		}
		if orig.Pointer() == cp.Pointer() {
			t.Errorf("Clone shares %s with the original; copy it in Clone", f.Name)
		}
	}
	if (*User)(nil).Clone() != nil {
		t.Error("Clone of nil isn't nil")
	}
	// A nil field stays nil.
	if c := (&User{}).Clone(); c.Roles != nil || c.NamedFlags != nil || c.DeletedAt != nil {
		t.Errorf("Clone made nil fields non-nil: %+v", c)
	}
}

// A getter's copy edited in place leaves the stored record as it was.
func TestGetterCopiesAreIndependent(t *testing.T) {
	deleted := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	um, _ := mgrWithUser(t, &User{
		ID: 2, Handle: "Val", AccessLevel: 20,
		Roles:              []string{"validator"},
		NamedFlags:         map[string]string{"donor": ""},
		LastReadMessageIDs: map[int]string{1: "a"},
		PublicKeys:         []string{"key"},
		DeletedAt:          &deleted,
	})
	for name, get := range map[string]func() *User{
		"GetUserByID": func() *User { u, _ := um.GetUserByID(2); return u },
		"GetUser":     func() *User { u, _ := um.GetUser("val"); return u },
		"GetAllUsers": func() *User { return um.GetAllUsers()[0] },
	} {
		u := get()
		u.Roles[0] = "admin"
		u.NamedFlags["beta"] = ""
		u.LastReadMessageIDs[1] = "b"
		u.PublicKeys[0] = "other"
		*u.DeletedAt = time.Time{}

		again, _ := um.GetUserByID(2)
		if again.Roles[0] != "validator" || len(again.NamedFlags) != 1 || again.LastReadMessageIDs[1] != "a" ||
			again.PublicKeys[0] != "key" || !again.DeletedAt.Equal(deleted) {
			t.Errorf("%s: editing the copy changed the stored record: %+v", name, again)
		}
	}
}

// AddUser hands back a copy too: the new-user flow fills it in and saves it
// with UpdateUser, and until then the stored record is as AddUser left it.
func TestAddUserReturnsACopy(t *testing.T) {
	um, _ := mgrWithUser(t, &User{ID: 1, Handle: "Sysop", AccessLevel: 255})
	u, err := um.AddUser("password", "Newbie", "New Bie", "Here")
	if err != nil {
		t.Fatal(err)
	}
	u.PrivateNote = "set before the save"
	if stored, _ := um.GetUserByID(u.ID); stored.PrivateNote != "" {
		t.Fatalf("AddUser returned the stored record")
	}
	if err := um.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	if stored, _ := um.GetUserByID(u.ID); stored.PrivateNote != "set before the save" {
		t.Errorf("UpdateUser didn't store the copy: %q", stored.PrivateNote)
	}
}

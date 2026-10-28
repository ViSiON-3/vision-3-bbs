package user

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClampScreenWidth(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{-1, 0}, {0, 0}, {1, 80}, {40, 80}, {79, 80}, {80, 80}, {132, 132}, {255, 255}, {256, 255},
	} {
		if got := ClampScreenWidth(tc.in); got != tc.want {
			t.Errorf("ClampScreenWidth(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// A width stored before the minimum was 80 is raised to 80 on load.
func TestLoadRaisesNarrowScreenWidth(t *testing.T) {
	dir := t.TempDir()
	data := `[{"id":1,"handle":"Narrow","screenWidth":40},{"id":2,"handle":"Unset"},{"id":3,"handle":"Wide","screenWidth":132}]`
	if err := os.WriteFile(filepath.Join(dir, "users.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	um, err := NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	for handle, want := range map[string]int{"Narrow": 80, "Unset": 0, "Wide": 132} {
		u, ok := um.GetUser(handle)
		if !ok {
			t.Fatalf("%s not loaded", handle)
		}
		if u.ScreenWidth != want {
			t.Errorf("%s ScreenWidth = %d, want %d", handle, u.ScreenWidth, want)
		}
	}
}

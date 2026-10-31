package toast

import (
	"testing"
)

func TestStoreKeepsOrderAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	st := Open(dir)
	for _, text := range []string{"one", "two", "three"} {
		if err := st.Enqueue(7, Toast{Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Enqueue(8, Toast{Text: "other user"}); err != nil {
		t.Fatal(err)
	}

	// A new Store on the same file reads what a restart would.
	restarted := &Store{path: st.path, claimed: map[string]bool{}}
	got, err := restarted.Peek(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Text != "one" || got[1].Text != "two" || got[2].Text != "three" {
		t.Fatalf("queue = %+v, want one, two, three", got)
	}
	if got[0].ID == "" || got[0].ID == got[1].ID || got[0].CreatedAt.IsZero() {
		t.Errorf("IDs and times not filled in: %+v", got)
	}
}

func TestRemoveTakesOnlyTheNamedToast(t *testing.T) {
	st := Open(t.TempDir())
	_ = st.Enqueue(1, Toast{ID: "a", Text: "a"})
	_ = st.Enqueue(1, Toast{ID: "b", Text: "b"})

	if err := st.Remove(1, "a"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Peek(1)
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("queue = %+v, want b", got)
	}
	if err := st.Remove(1, "b"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Peek(1); len(got) != 0 {
		t.Fatalf("queue = %+v, want empty", got)
	}
}

func TestClaimIsExclusiveUntilReleased(t *testing.T) {
	st := Open(t.TempDir())
	if err := st.Enqueue(1, Toast{ID: "a", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if !st.Claim(1, "a") {
		t.Fatal("first claim refused")
	}
	if st.Claim(1, "a") {
		t.Fatal("second claim allowed")
	}
	st.Release("a")
	if !st.Claim(1, "a") {
		t.Fatal("claim refused after release")
	}
}

// Two sessions can Peek the same toast. Once one has shown and removed it, the
// other's copy is stale and must not be claimed.
func TestClaimRefusesARemovedToast(t *testing.T) {
	st := Open(t.TempDir())
	if err := st.Enqueue(1, Toast{ID: "a", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if !st.Claim(1, "a") {
		t.Fatal("first claim refused")
	}
	if err := st.Remove(1, "a"); err != nil {
		t.Fatal(err)
	}
	st.Release("a")
	if st.Claim(1, "a") {
		t.Fatal("claimed a toast already removed")
	}
}

package menu

import (
	"testing"
)

func TestComputeFilePagination(t *testing.T) {
	// Header of 3 lines, footer of 2 lines: headerLines=4, footerLines=3,
	// promptLines=2 (fixed), so fixedLines=9 and filesPerPage=termHeight-9.
	top := []byte("line1\nline2\nline3\n")
	bot := []byte("foot1\nfoot2\n")

	got := computeFilePagination(80, 24, top, bot)
	if got != 15 {
		t.Fatalf("computeFilePagination(80, 24, ...) = %d, want 15", got)
	}

	small := computeFilePagination(80, 10, top, bot)
	if small != 1 {
		t.Fatalf("computeFilePagination(80, 10, ...) = %d, want 1", small)
	}
	if small >= got {
		t.Fatalf("smaller terminal must page fewer files: %d >= %d", small, got)
	}
}

func TestComputeFilePagination_ClampsToAtLeastOne(t *testing.T) {
	top := []byte("line1\nline2\nline3\n")
	bot := []byte("foot1\nfoot2\n")

	// termHeight smaller than fixedLines (9) must still yield at least 1.
	got := computeFilePagination(80, 1, top, bot)
	if got != 1 {
		t.Fatalf("computeFilePagination(80, 1, ...) = %d, want 1 (clamped)", got)
	}
}

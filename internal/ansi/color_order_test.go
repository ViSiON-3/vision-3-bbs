package ansi

import "testing"

// ColorOrder swaps DOS and ANSI order both ways: DOS 1 blue is ANSI 4, DOS
// 3 cyan is ANSI 6, and back.
func TestColorOrder(t *testing.T) {
	for dos, ansi := range map[int]int{0: 0, 1: 4, 2: 2, 3: 6, 4: 1, 5: 5, 6: 3, 7: 7, 11: 6, 9: 4} {
		if got := ColorOrder(dos); got != ansi {
			t.Errorf("ColorOrder(%d) = %d, want %d", dos, got, ansi)
		}
		if got := ColorOrder(ColorOrder(dos)); got != dos&7 {
			t.Errorf("ColorOrder twice on %d = %d", dos, got)
		}
	}
}

func TestPipeBackground(t *testing.T) {
	for code, want := range map[string]int{"|00|B6": 6, "|15|B4": 4, "|14|B12": 4, "|B1": 1} {
		if got, ok := PipeBackground(code); !ok || got != want {
			t.Errorf("PipeBackground(%q) = %d, %v, want %d", code, got, ok, want)
		}
	}
	for _, code := range []string{"|07", "", "|C7", "|B", "|BX"} {
		if _, ok := PipeBackground(code); ok {
			t.Errorf("PipeBackground(%q) found a background", code)
		}
	}
}

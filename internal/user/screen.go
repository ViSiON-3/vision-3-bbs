package user

// Screen width limits for the stored ScreenWidth. Screens are laid out for
// an 80×25 terminal, so no narrower width is kept: a caller on a smaller
// terminal still gets the 80-column layout.
const (
	MinScreenWidth = 80
	MaxScreenWidth = 255
)

// ClampScreenWidth returns w within MinScreenWidth..MaxScreenWidth. Zero and
// below mean "not set" and come back as 0.
func ClampScreenWidth(w int) int {
	switch {
	case w <= 0:
		return 0
	case w < MinScreenWidth:
		return MinScreenWidth
	case w > MaxScreenWidth:
		return MaxScreenWidth
	}
	return w
}

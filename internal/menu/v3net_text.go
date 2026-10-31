package menu

import "github.com/ViSiON-3/vision-3-bbs/internal/ansi"

// Text from a hub or another node (area names and tags, BBS names, proposal
// fields, error text quoting a hub) goes onto pipe-coded screens through these.
// None of it was meant to carry colour, unlike chat (see chatText), so every pipe
// prints as written and control characters become spaces.

// v3netText prepares hub text for a pipe-coded line.
func v3netText(s string) string {
	return ansi.EscapePipes(ansi.OneLine(s))
}

// v3netClip is v3netText clipped to width columns. Clipping comes first: "||"
// draws as one column, and a cut through it would leave half a pipe code.
func v3netClip(s string, width int) string {
	return ansi.EscapePipes(truncateStr(ansi.OneLine(s), width))
}

// v3netCell is v3netClip padded to exactly width columns, for a table column:
// a %-20s would count a doubled pipe as two.
func v3netCell(s string, width int) string {
	return ansi.EscapePipes(padRight(truncateStr(ansi.OneLine(s), width), width))
}

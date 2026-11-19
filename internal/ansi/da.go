package ansi

// DAExtendedQuery asks for CTerm's device attributes (CTDA). CTerm replies
// CSI < 0 ; Ps... c, listing its features; feature 4 is pixel operations,
// sixel among them (syncterm.bbsdev.net/cterm.html, "CSI < Ps c").
// Terminals that do not know it ignore it.
const DAExtendedQuery = "\x1b[<c"

// DAPrimaryQuery asks for the primary device attributes (DA1). A DEC-style
// terminal replies CSI ? Pclass ; Ps... c, where attribute 4 is sixel
// graphics; CTerm replies CSI = 67;84;101;114;109; and its revision with
// dots as semicolons, then c. It is sent after DAExtendedQuery, so its reply
// is the last one to wait for.
const DAPrimaryQuery = "\x1b[c"

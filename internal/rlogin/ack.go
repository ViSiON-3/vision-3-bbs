package rlogin

import "io"

// AckReader strips the single NUL byte an rlogin server sends to acknowledge
// the handshake, and is otherwise transparent.
//
// The acknowledgement is deliberately handled in the read path rather than by
// reading one byte after connecting. Not every door server sends it --
// Synchronet does not wait for one and passes it straight through to the
// terminal -- so a blocking read would hang against those servers, and a read
// with a timeout would either add a round trip to every connection or race a
// slow one. Folding it into the first read costs nothing and cannot stall.
//
// Only a NUL in the very first byte position of the session is consumed. A NUL
// arriving later is door output and is passed through untouched.
type AckReader struct {
	r       io.Reader
	checked bool // the first byte has been inspected; never strip again
}

// NewAckReader wraps r so that a leading acknowledgement byte is discarded.
func NewAckReader(r io.Reader) *AckReader {
	return &AckReader{r: r}
}

func (a *AckReader) Read(p []byte) (int, error) {
	if a.checked {
		return a.r.Read(p)
	}
	if len(p) == 0 {
		return 0, nil
	}
	// Loop so a first read consisting of nothing but the ack byte does not
	// surface as a zero-length read, which a caller may treat as a spin.
	for {
		n, err := a.r.Read(p)
		if n > 0 {
			if !a.checked {
				a.checked = true
				if p[0] == 0 {
					n = copy(p, p[1:n])
				}
			}
			if n > 0 || err != nil {
				return n, err
			}
			continue // the ack was the whole read; wait for real output
		}
		if err != nil {
			return 0, err
		}
	}
}

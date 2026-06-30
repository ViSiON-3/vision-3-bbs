package admin

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Frame is one message on the admin RPC channel.
type Frame struct {
	Kind     string          `json:"kind"`
	Snapshot *SystemSnapshot `json:"snapshot,omitempty"`
	Event    *Event          `json:"event,omitempty"`
	Command  *AdminCommand   `json:"command,omitempty"`
	Result   *Result         `json:"result,omitempty"`
	Err      string          `json:"err,omitempty"`
}

const (
	KindHello    = "hello"
	KindSnapshot = "snapshot"
	KindEvent    = "event"
	KindCommand  = "command"
	KindResult   = "result"
	KindError    = "error"
)

const maxFrameBytes = 1 << 20 // 1 MiB

// WriteFrame writes a length-prefixed JSON frame.
func WriteFrame(w io.Writer, f *Frame) error {
	payload, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if len(payload) > maxFrameBytes {
		return fmt.Errorf("admin: frame too large: %d bytes", len(payload))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

// ReadFrame reads one length-prefixed JSON frame.
func ReadFrame(r io.Reader) (*Frame, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxFrameBytes {
		return nil, fmt.Errorf("admin: frame too large: %d bytes", n)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	var f Frame
	if err := json.Unmarshal(payload, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

package jam

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Little-endian binary read/write helpers shared by the JAM record codecs.
// JAM stores every numeric field little-endian regardless of host byte order.

func readBinaryLE(r io.Reader, data interface{}, label string) error {
	if err := binary.Read(r, binary.LittleEndian, data); err != nil {
		return fmt.Errorf("jam: read %s: %w", label, err)
	}
	return nil
}

func writeBinaryLE(w io.Writer, data interface{}, label string) error {
	if err := binary.Write(w, binary.LittleEndian, data); err != nil {
		return fmt.Errorf("jam: write %s: %w", label, err)
	}
	return nil
}

func writeAll(w io.Writer, buffer []byte, label string) error {
	n, err := w.Write(buffer)
	if err != nil {
		return fmt.Errorf("jam: write %s: %w", label, err)
	}
	if n != len(buffer) {
		return fmt.Errorf("jam: short write %s: wrote %d of %d bytes", label, n, len(buffer))
	}
	return nil
}

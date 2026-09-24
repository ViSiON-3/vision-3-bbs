package qwk

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func buildZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		if err := writeZipEntry(zw, name, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func padLeft(n, width int) string {
	s := fmt.Sprint(n)
	return strings.Repeat(" ", width-len(s)) + s
}

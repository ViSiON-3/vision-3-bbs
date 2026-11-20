//go:build !unix

package archiver

import "errors"

func mkfifo(string) error { return errors.New("not supported") }

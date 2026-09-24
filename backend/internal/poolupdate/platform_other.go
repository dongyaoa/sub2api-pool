//go:build !linux

package poolupdate

import (
	"errors"
	"os"
)

func checkRootOwner(os.FileInfo) error { return nil }
func syncDirectory(string) error       { return nil }
func AcquireLock(string) (*os.File, error) {
	return nil, errors.New("pool updater host is supported only on Linux")
}

//go:build !linux && !darwin && !windows

package main

import (
	"fmt"
	"runtime"
)

// openFolderNative is unsupported on this platform.
func openFolderNative(string) error {
	return fmt.Errorf("opening folders is not supported on %s", runtime.GOOS)
}

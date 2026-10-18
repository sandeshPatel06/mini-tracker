//go:build linux || darwin

package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openFolderNative opens a directory in the platform file manager.
func openFolderNative(path string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if _, err := exec.LookPath(opener); err != nil {
		return fmt.Errorf("%s not found: %w", opener, err)
	}
	return exec.Command(opener, path).Start()
}

//go:build windows

package main

import "os/exec"

// openFolderNative opens a directory in Windows Explorer.
func openFolderNative(path string) error {
	return exec.Command("cmd", "/c", "start", "", path).Start()
}

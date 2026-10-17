//go:build !linux && !darwin && !windows

package main

import "syscall"

// detachSysProcAttr is a no-op on unsupported platforms.
func detachSysProcAttr() *syscall.SysProcAttr {
	return nil
}

func processAlive(pid int) bool {
	return false
}

func terminateProcess(pid int) error {
	return nil
}

func killProcess(pid int) error {
	return nil
}

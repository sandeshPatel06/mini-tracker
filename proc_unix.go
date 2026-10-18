//go:build linux || darwin

package main

import "syscall"

// detachSysProcAttr detaches the child into its own session.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func terminateProcess(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

func killProcess(pid int) error {
	return syscall.Kill(pid, syscall.SIGKILL)
}

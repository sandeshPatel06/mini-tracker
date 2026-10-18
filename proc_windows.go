//go:build windows

package main

import (
	"syscall"

	"golang.org/x/sys/windows"
)

const (
	detachedProcessFlag       = 0x00000008
	createNewProcessGroupFlag = 0x00000200
)

// detachSysProcAttr detaches the child from the parent console and process group.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: detachedProcessFlag | createNewProcessGroupFlag}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func terminateProcess(pid int) error {
	return terminateWindowsProcess(pid)
}

func killProcess(pid int) error {
	return terminateWindowsProcess(pid)
}

func terminateWindowsProcess(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

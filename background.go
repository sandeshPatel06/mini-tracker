package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/reak/get-hike/internal/config"
)

// runBackgroundApp runs the headless tracking daemon. It reuses App.startup /
// App.shutdown / App.collect unchanged so behavior matches the GUI app.
func runBackgroundApp() {
	cfg, err := config.Load()
	if err == nil && cfg != nil {
		pid := readPIDFile(pidPath(cfg.DataDir))
		if pid > 0 && pid != os.Getpid() && processAlive(pid) && processMatchesDaemon(pid) {
			log.Printf("[bg] another background tracker is already running (pid %d) — exiting", pid)
			return
		}
	}

	app := NewApp()
	app.headless = true
	app.startup(context.Background())

	if app.cfg != nil {
		if err := writePIDFile(pidPath(app.cfg.DataDir), os.Getpid()); err != nil {
			log.Printf("[bg] write pidfile error: %v", err)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigCh
	log.Printf("[bg] received signal %v — shutting down", sig)

	app.shutdown(context.Background())

	if app.cfg != nil {
		_ = os.Remove(pidPath(app.cfg.DataDir))
	}
}

// pidPath returns the pidfile location for a data directory.
func pidPath(dataDir string) string {
	return filepath.Join(dataDir, "tracker.pid")
}

// writePIDFile persists pid to path.
func writePIDFile(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
}

// readPIDFile reads a pid from path, returning 0 on any error.
func readPIDFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// processMatchesDaemon verifies on Linux that pid is indeed our --background daemon.
func processMatchesDaemon(pid int) bool {
	if runtime.GOOS != "linux" {
		return true
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "--background")
}

// stopBackgroundDaemon terminates a running background tracker for dataDir, if any.
func stopBackgroundDaemon(dataDir string) {
	path := pidPath(dataDir)
	pid := readPIDFile(path)
	if pid <= 0 {
		_ = os.Remove(path)
		return
	}
	if !processAlive(pid) {
		_ = os.Remove(path)
		return
	}

	_ = terminateProcess(pid)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if processAlive(pid) {
		_ = killProcess(pid)
	}
	_ = os.Remove(path)
	log.Printf("[bg] stopped background tracker (pid %d)", pid)
}

// spawnBackgroundDaemon starts a detached copy of this binary in headless mode.
func spawnBackgroundDaemon(dataDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, "--background")
	cmd.SysProcAttr = detachSysProcAttr()
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		return err
	}

	if err := writePIDFile(pidPath(dataDir), cmd.Process.Pid); err != nil {
		log.Printf("[bg] write pidfile error: %v", err)
	}
	return cmd.Process.Release()
}

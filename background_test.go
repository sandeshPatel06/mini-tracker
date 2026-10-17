package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPIDFileRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := pidPath(dir)

	if err := writePIDFile(path, 12345); err != nil {
		t.Fatalf("writePIDFile() error = %v", err)
	}
	if got := readPIDFile(path); got != 12345 {
		t.Fatalf("readPIDFile() = %d, want 12345", got)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("os.Remove() error = %v", err)
	}
	if got := readPIDFile(path); got != 0 {
		t.Fatalf("readPIDFile() after remove = %d, want 0", got)
	}
}

func TestPIDFileMissingAndGarbage(t *testing.T) {
	dir := t.TempDir()

	if got := readPIDFile(filepath.Join(dir, "missing.pid")); got != 0 {
		t.Fatalf("readPIDFile() on missing file = %d, want 0", got)
	}

	garbagePath := filepath.Join(dir, "garbage.pid")
	if err := os.WriteFile(garbagePath, []byte("not-a-pid\n"), 0644); err != nil {
		t.Fatalf("write garbage pidfile error = %v", err)
	}
	if got := readPIDFile(garbagePath); got != 0 {
		t.Fatalf("readPIDFile() on garbage = %d, want 0", got)
	}
}

func TestPIDProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("processAlive(self) = false, want true")
	}
	if processAlive(999999) {
		t.Fatal("processAlive(999999) = true, want false")
	}
}

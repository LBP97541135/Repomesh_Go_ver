//go:build unix

package secrets

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestRootFilePermissions(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "root")
	if err := os.WriteFile(good, bytes.Repeat([]byte{31}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := readRootFile(good); err != nil || len(got) != 32 {
		t.Fatal("valid root file was rejected")
	}
	for _, mode := range []os.FileMode{0644, 0640, 0400, 0660, 0700} {
		if err := os.Chmod(good, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := readRootFile(good); err == nil {
			t.Fatalf("unsafe mode %o was accepted", mode)
		}
	}
	if err := os.Chmod(good, 0600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "link")
	if err := os.Symlink(good, symlink); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{symlink, fifo, dir, "relative-root", filepath.Join(dir, "missing-secret-path")} {
		got, err := readRootFile(path)
		if err == nil || got != nil {
			t.Fatal("invalid file kind was accepted")
		}
		if strings.Contains(err.Error(), path) {
			t.Fatal("root path leaked in error")
		}
	}
	for _, length := range []int{0, 31, 33, 1024} {
		if err := os.WriteFile(good, bytes.Repeat([]byte{31}, length), 0600); err != nil {
			t.Fatal(err)
		}
		if got, err := readRootFile(good); err == nil || got != nil {
			t.Fatalf("root length %d was accepted", length)
		}
	}
}

func TestRootFileOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(path, make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, os.Geteuid()+1, -1); err != nil {
		t.Skip("changing file ownership requires privilege")
	}
	if got, err := readRootFile(path); err == nil || got != nil {
		t.Fatal("another owner's root was accepted")
	}
}

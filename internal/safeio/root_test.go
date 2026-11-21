//go:build darwin || linux

package safeio

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestBoundedReadAndLinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module safe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("go.mod", filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "outside")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(dir, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, name := range []string{"alias", "outside/secret", ".env", "fifo", "../go.mod"} {
		if _, err := r.Read(name, 1024); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
	if _, err := r.Read("go.mod", 3); err == nil {
		t.Fatal("read exceeded budget")
	}
	if b, err := r.Read("go.mod", 100); err != nil || string(b) != "module safe" {
		t.Fatalf("read %q: %v", b, err)
	}
}

func TestPathValidation(t *testing.T) {
	for _, name := range []string{"", "/etc/passwd", "a/../b", "a//b", "C:/x", `a\b`, "CON", "a/NUL.json", "x. "} {
		if ValidatePath(name) == nil {
			t.Errorf("accepted %q", name)
		}
	}
}

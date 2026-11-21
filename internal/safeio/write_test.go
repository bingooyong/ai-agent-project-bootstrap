//go:build darwin || linux

package safeio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func writeRoot(t *testing.T) (*Root, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r, dir
}

func assertFile(t *testing.T, dir, name, want string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil || string(b) != want {
		t.Fatalf("%s: got %q, %v; want %q", name, b, err, want)
	}
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".ombp-tmp-") {
			t.Errorf("temporary file leaked: %s", entry.Name())
		}
	}
}

func TestMkdirWithoutFollowingLinks(t *testing.T) {
	r, dir := writeRoot(t)
	if err := r.MkdirAll(".ombp/state/nested", 0700); err != nil {
		t.Fatal(err)
	}
	if err := r.MkdirAll(".ombp/state/nested", 0700); err != nil {
		t.Fatal("existing directories should be accepted:", err)
	}
	info, err := os.Stat(filepath.Join(dir, ".ombp/state/nested"))
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatalf("unexpected directory: %v, %v", info, err)
	}
	if err := r.MkdirExclusive(".ombp/state/mutex", 0700); err != nil {
		t.Fatal(err)
	}
	if err := r.MkdirExclusive(".ombp/state/mutex", 0700); err == nil {
		t.Fatal("exclusive mkdir accepted existing directory")
	}
	if err := os.Symlink(".ombp", filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"linked", "linked/new", "../outside", ".env/nested", "secrets/new"} {
		if err := r.MkdirAll(name, 0700); err == nil {
			t.Errorf("mkdir accepted unsafe path %q", name)
		}
		if err := r.MkdirExclusive(name, 0700); err == nil {
			t.Errorf("exclusive mkdir accepted unsafe path %q", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".ombp/new")); !os.IsNotExist(err) {
		t.Fatalf("followed directory link: %v", err)
	}
}

func TestReplaceCreateAndCompareDigest(t *testing.T) {
	r, dir := writeRoot(t)
	if err := r.Replace("AGENTS.md", []byte("first"), ""); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "AGENTS.md"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected file permissions: %v, %v", info, err)
	}
	if err := r.Replace("AGENTS.md", []byte("overwrite"), ""); err == nil {
		t.Fatal("create-only replace overwrote target")
	}
	if err := r.Replace("AGENTS.md", []byte("overwrite"), Digest([]byte("other"))); err == nil {
		t.Fatal("replace accepted stale digest")
	}
	assertFile(t, dir, "AGENTS.md", "first")
	if err := r.Replace("AGENTS.md", []byte("second"), Digest([]byte("first"))); err != nil {
		t.Fatal(err)
	}
	assertFile(t, dir, "AGENTS.md", "second")
	for _, name := range []string{".", "../outside", ".env", "secrets/data", "a//b"} {
		if err := r.Replace(name, []byte("bad"), ""); err == nil {
			t.Errorf("replace accepted unsafe path %q", name)
		}
	}
	if err := r.Replace("AGENTS.md", []byte("bad"), "not-a-digest"); err == nil {
		t.Fatal("replace accepted malformed digest")
	}
	assertNoTemporaryFiles(t, dir)
}

func TestReplaceRejectsLinksAndSpecialFiles(t *testing.T) {
	r, dir := writeRoot(t)
	if err := os.WriteFile(filepath.Join(dir, "original"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("original", filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "real"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(dir, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alias", "linked/file", "fifo", "real"} {
		for _, expected := range []string{"", Digest([]byte("keep"))} {
			if err := r.Replace(name, []byte("bad"), expected); err == nil {
				t.Errorf("replace accepted %q", name)
			}
		}
	}
	assertFile(t, dir, "original", "keep")
	if _, err := os.Stat(filepath.Join(dir, "real/file")); !os.IsNotExist(err) {
		t.Fatalf("followed intermediate link: %v", err)
	}
	assertNoTemporaryFiles(t, dir)
}

func TestConcurrentCreateDoesNotOverwrite(t *testing.T) {
	r, dir := writeRoot(t)
	const count = 24
	start := make(chan struct{})
	type result struct {
		value string
		err   error
	}
	results := make(chan result, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			value := fmt.Sprintf("writer-%d", i)
			results <- result{value, r.Replace("new.md", []byte(value), "")}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	winners := []string{}
	for result := range results {
		if result.err == nil {
			winners = append(winners, result.value)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("got %d successful creators; want exactly one", len(winners))
	}
	assertFile(t, dir, "new.md", winners[0])
	assertNoTemporaryFiles(t, dir)
}

func TestRemoveOnlyMatchingRegularFiles(t *testing.T) {
	r, dir := writeRoot(t)
	if err := r.Replace("keep.md", []byte("keep"), ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("keep.md", filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := r.MkdirAll("real", 0700); err != nil {
		t.Fatal(err)
	}
	if err := r.Replace("real/item", []byte("keep"), ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alias", "linked/item", "real", ".", "../outside", ".env"} {
		if err := r.RemoveIf(name, Digest([]byte("keep"))); err == nil {
			t.Errorf("remove accepted %q", name)
		}
	}
	if err := r.RemoveIf("keep.md", Digest([]byte("changed"))); err == nil {
		t.Fatal("remove accepted stale digest")
	}
	assertFile(t, dir, "keep.md", "keep")
	assertFile(t, dir, "real/item", "keep")
	if err := r.RemoveIf("keep.md", Digest([]byte("keep"))); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveIf("keep.md", Digest([]byte("keep"))); err != nil {
		t.Fatal("repeat remove should accept absent file:", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "alias")); err != nil {
		t.Fatal("link itself was removed:", err)
	}
}

func TestRemoveEmptyDirectoriesOnly(t *testing.T) {
	r, dir := writeRoot(t)
	if err := r.MkdirAll("parent/empty", 0700); err != nil {
		t.Fatal(err)
	}
	if err := r.Replace("parent/file", []byte("keep"), ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("parent/empty", filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"parent", "parent/file", "alias", ".", "../outside", ".env"} {
		if err := r.RemoveEmptyDir(name); err == nil {
			t.Errorf("removed nonempty or unsafe directory %q", name)
		}
	}
	if err := r.RemoveEmptyDir("parent/empty"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveEmptyDir("parent/empty"); err != nil {
		t.Fatal("repeat empty directory removal:", err)
	}
	assertFile(t, dir, "parent/file", "keep")
}

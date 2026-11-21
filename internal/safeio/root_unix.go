//go:build darwin || linux

package safeio

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

type Root struct {
	file   *os.File
	meta   *os.Root
	device uint64
}

// OpenExternalParent preserves policy for a caller-selected output/plan path.
// Unlike a selected project root, user-created ancestor links are not resolved.
// Darwin's fixed OS aliases are normalized explicitly so /var and /tmp work.
func OpenExternalParent(name string) (*Root, string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return nil, "", err
	}
	if Sensitive(filepath.ToSlash(abs)) {
		return nil, "", errors.New("sensitive external path refused")
	}
	if runtime.GOOS == "darwin" {
		for alias, target := range map[string]string{"/var": "/private/var", "/tmp": "/private/tmp", "/etc": "/private/etc"} {
			if abs == alias || strings.HasPrefix(abs, alias+"/") {
				resolved, e := filepath.EvalSymlinks(alias)
				if e != nil || resolved != target {
					return nil, "", errors.New("unexpected system path alias")
				}
				abs = target + strings.TrimPrefix(abs, alias)
				break
			}
		}
	}
	base := filepath.Base(abs)
	if err := ValidatePath(base); err != nil {
		return nil, "", err
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(abs), "/"), "/") {
		if part == "" {
			continue
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(fd)
		if e != nil {
			return nil, "", errors.New("external parent contains a link or inaccessible directory")
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), "external-parent")
	meta, e := os.OpenRoot(filepath.Dir(abs))
	if e != nil {
		_ = f.Close()
		return nil, "", e
	}
	a, e := f.Stat()
	b, e2 := meta.Stat(".")
	if e != nil || e2 != nil || !os.SameFile(a, b) {
		_ = f.Close()
		_ = meta.Close()
		return nil, "", errors.New("external parent changed while opening")
	}
	var st unix.Stat_t
	if e := unix.Fstat(fd, &st); e != nil {
		_ = f.Close()
		_ = meta.Close()
		return nil, "", e
	}
	return &Root{file: f, meta: meta, device: uint64(st.Dev)}, base, nil
}

func Open(name string) (*Root, error) {
	absolute, err := filepath.EvalSymlinks(name)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(absolute, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "project-root")
	m, err := os.OpenRoot(absolute)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	a, err := f.Stat()
	if err != nil {
		_ = f.Close()
		_ = m.Close()
		return nil, err
	}
	b, err := m.Stat(".")
	if err != nil || !os.SameFile(a, b) {
		_ = f.Close()
		_ = m.Close()
		return nil, errors.New("project root changed while opening")
	}
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		_ = f.Close()
		_ = m.Close()
		return nil, err
	}
	return &Root{file: f, meta: m, device: uint64(st.Dev)}, nil
}

func (r *Root) Close() error { return errors.Join(r.file.Close(), r.meta.Close()) }

// open traverses each component through an anchored descriptor with NOFOLLOW.
// NONBLOCK prevents an adversarial FIFO from hanging before Fstat rejects it.
func (r *Root) open(name string, directory bool) (*os.File, error) {
	if err := ValidatePath(name); err != nil {
		return nil, err
	}
	parts := strings.Split(name, "/")
	current := int(r.file.Fd())
	owned := -1
	defer func() {
		if owned >= 0 {
			_ = unix.Close(owned)
		}
	}()
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if i < len(parts)-1 || directory {
			flags |= unix.O_DIRECTORY
		}
		fd, err := unix.Openat(current, part, flags, 0)
		if err != nil {
			return nil, fmt.Errorf("safe open %s: %w", name, err)
		}
		var st unix.Stat_t
		if err = unix.Fstat(fd, &st); err != nil {
			_ = unix.Close(fd)
			return nil, err
		}
		if uint64(st.Dev) != r.device {
			_ = unix.Close(fd)
			return nil, errors.New("cross-device traversal refused")
		}
		if owned >= 0 {
			_ = unix.Close(owned)
		}
		owned = fd
		current = fd
	}
	f := os.NewFile(uintptr(owned), name)
	owned = -1
	return f, nil
}

func (r *Root) Read(name string, limit int64) ([]byte, error) {
	if Sensitive(name) {
		return nil, errors.New("sensitive path refused")
	}
	if limit <= 0 || limit > 4<<20 {
		return nil, errors.New("invalid read budget")
	}
	f, err := r.open(name, false)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("nonregular file refused")
	}
	if info.Size() > limit {
		return nil, errors.New("file read budget exceeded")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("file read budget exceeded")
	}
	return b, nil
}

func (r *Root) ReadDir(name string) ([]fs.DirEntry, error) {
	return r.ReadDirLimit(name, 20000)
}

// ReadDirLimit caps enumeration before allocating an unbounded directory list.
func (r *Root) ReadDirLimit(name string, limit int) ([]fs.DirEntry, error) {
	if limit <= 0 || limit > 20001 {
		return nil, errors.New("invalid directory entry budget")
	}
	if Sensitive(name) {
		return nil, errors.New("sensitive directory refused")
	}
	f, err := r.open(name, true)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries := []fs.DirEntry{}
	for {
		batch, e := f.ReadDir(min(256, limit-len(entries)+1))
		entries = append(entries, batch...)
		if len(entries) > limit {
			return nil, errors.New("directory entry budget exceeded")
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

// Lstat only obtains metadata. All content reads separately use open above.
func (r *Root) Lstat(name string) (fs.FileInfo, error) {
	if err := ValidatePath(name); err != nil {
		return nil, err
	}
	return r.meta.Lstat(name)
}

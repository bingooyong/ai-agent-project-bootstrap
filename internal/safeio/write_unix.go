//go:build darwin || linux

package safeio

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

const maxWriteBytes = 4 << 20

// Write methods anchor all operations to directory descriptors and reject
// symlinks. They cooperate with the updater's journal and exclusive lock; they
// are not a multi-file transaction. A digest comparison and rename/unlink are
// separate syscalls, so they do not protect against a malicious concurrent
// editor. An attacker can also move an opened parent directory out of the root;
// callers must not claim protection against malicious directory renames.

func writePath(name string, allowRoot bool) error {
	if err := ValidatePath(name); err != nil {
		return err
	}
	if name == "." && !allowRoot {
		return errors.New("root directory mutation refused")
	}
	if Sensitive(name) {
		return errors.New("sensitive path refused")
	}
	return nil
}

func directoryMode(perm fs.FileMode) (uint32, error) {
	if perm & ^fs.FileMode(0777) != 0 {
		return 0, errors.New("invalid directory permissions")
	}
	return uint32(perm.Perm()), nil
}

// MkdirAll creates directories one component at a time without following links.
// Existing directories retain their permissions.
func (r *Root) MkdirAll(name string, perm fs.FileMode) error {
	if err := writePath(name, true); err != nil {
		return err
	}
	mode, err := directoryMode(perm)
	if err != nil {
		return err
	}
	if name == "." {
		return nil
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		parent, err := r.open(path.Dir(prefix), true)
		if err != nil {
			return err
		}
		created := false
		err = unix.Mkdirat(int(parent.Fd()), parts[i], mode)
		if err == nil {
			created = true
		} else if !errors.Is(err, unix.EEXIST) {
			_ = parent.Close()
			return fmt.Errorf("create directory %s: %w", prefix, err)
		}
		child, openErr := r.open(prefix, true)
		if openErr != nil {
			_ = parent.Close()
			return openErr
		}
		err = child.Close()
		if created {
			err = errors.Join(err, parent.Sync())
		}
		err = errors.Join(err, parent.Close())
		if err != nil {
			return err
		}
	}
	return nil
}

// MkdirExclusive atomically creates a previously absent directory. The parent
// must already exist; EEXIST is returned for every existing target type.
func (r *Root) MkdirExclusive(name string, perm fs.FileMode) error {
	if err := writePath(name, false); err != nil {
		return err
	}
	mode, err := directoryMode(perm)
	if err != nil {
		return err
	}
	parent, err := r.open(path.Dir(name), true)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := unix.Mkdirat(int(parent.Fd()), path.Base(name), mode); err != nil {
		return fmt.Errorf("create exclusive directory %s: %w", name, err)
	}
	return parent.Sync()
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}

// readAt opens the final component without following links, bounds its content,
// and returns the opened inode so the caller can reject entry substitution.
func (r *Root) readAt(parent int, base string) ([]byte, unix.Stat_t, error) {
	var st unix.Stat_t
	fd, err := unix.Openat(parent, base, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, st, err
	}
	f := os.NewFile(uintptr(fd), base)
	defer f.Close()
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, st, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || uint64(st.Dev) != r.device {
		return nil, st, errors.New("nonregular or cross-device file refused")
	}
	if st.Size > maxWriteBytes {
		return nil, st, errors.New("file exceeds mutation budget")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxWriteBytes+1))
	if err != nil {
		return nil, st, err
	}
	if len(b) > maxWriteBytes {
		return nil, st, errors.New("file exceeds mutation budget")
	}
	return b, st, nil
}

func sameEntry(parent int, base string, old unix.Stat_t) error {
	var now unix.Stat_t
	if err := unix.Fstatat(parent, base, &now, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if now.Mode&unix.S_IFMT != unix.S_IFREG || now.Dev != old.Dev || now.Ino != old.Ino {
		return errors.New("target changed during digest verification")
	}
	return nil
}

// CheckCreate checks that an output has an existing safe parent and an absent
// destination. Replace must still use create-only mode to prevent a race.
func (r *Root) CheckCreate(name string) error {
	if err := writePath(name, false); err != nil {
		return err
	}
	parent, err := r.open(path.Dir(name), true)
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return ErrUnsafeOutput
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	var info unix.Stat_t
	err = unix.Fstatat(int(parent.Fd()), path.Base(name), &info, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode&unix.S_IFMT == unix.S_IFLNK {
		return ErrUnsafeOutput
	}
	return fs.ErrExist
}

// Replace writes through a 0600 exclusive temporary file. Empty expected means
// create-only: Linkat refuses any existing target atomically. Otherwise expected
// is the old SHA-256; the caller's journal must handle any error after mutation,
// including a directory sync failure.
func (r *Root) Replace(name string, data []byte, expected string) (result error) {
	if err := writePath(name, false); err != nil {
		return err
	}
	if len(data) > maxWriteBytes || (expected != "" && !validDigest(expected)) {
		return errors.New("invalid mutation budget or expected digest")
	}
	parent, err := r.open(path.Dir(name), true)
	if err != nil {
		return err
	}
	defer parent.Close()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temp := ".ombp-tmp-" + hex.EncodeToString(nonce[:])
	parentFD := int(parent.Fd())
	fd, err := unix.Openat(parentFD, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		err := unix.Unlinkat(parentFD, temp, 0)
		if err != nil && !errors.Is(err, unix.ENOENT) {
			result = errors.Join(result, fmt.Errorf("remove temporary file: %w", err))
		}
	}()
	f := os.NewFile(uintptr(fd), temp)
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = f.Sync()
	}
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return err
	}
	base := path.Base(name)
	if expected == "" {
		// Creating a link instead of rename makes the absent-target condition
		// atomic even when another process creates the target concurrently.
		if err := unix.Linkat(parentFD, temp, parentFD, base, 0); err != nil {
			return fmt.Errorf("create target without overwrite: %w", err)
		}
		if err := unix.Unlinkat(parentFD, temp, 0); err != nil {
			return err
		}
	} else {
		current, st, err := r.readAt(parentFD, base)
		if err != nil {
			return err
		}
		if Digest(current) != expected {
			return errors.New("target digest changed; replacement refused")
		}
		if err := sameEntry(parentFD, base, st); err != nil {
			return err
		}
		if err := unix.Renameat(parentFD, temp, parentFD, base); err != nil {
			return err
		}
	}
	return parent.Sync()
}

// RemoveIf removes a regular file only if its current digest matches expected.
// A missing target is already removed. This is cooperative CAS, not an atomic
// digest-and-unlink operation against malicious concurrent writers.
func (r *Root) RemoveIf(name string, expected string) error {
	if err := writePath(name, false); err != nil {
		return err
	}
	if !validDigest(expected) {
		return errors.New("invalid expected digest")
	}
	parent, err := r.open(path.Dir(name), true)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	parentFD, base := int(parent.Fd()), path.Base(name)
	current, st, err := r.readAt(parentFD, base)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if Digest(current) != expected {
		return errors.New("target digest changed; removal refused")
	}
	if err := sameEntry(parentFD, base, st); err != nil {
		return err
	}
	if err := unix.Unlinkat(parentFD, base, 0); err != nil {
		return err
	}
	return parent.Sync()
}

// RemoveEmptyDir never recursively removes content or follows a directory link.
func (r *Root) RemoveEmptyDir(name string) error {
	if err := writePath(name, false); err != nil {
		return err
	}
	parent, err := r.open(path.Dir(name), true)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	parentFD, base := int(parent.Fd()), path.Base(name)
	var st unix.Stat_t
	if err := unix.Fstatat(parentFD, base, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(st.Dev) != r.device {
		return errors.New("nondirectory or cross-device removal refused")
	}
	if err := unix.Unlinkat(parentFD, base, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	return parent.Sync()
}

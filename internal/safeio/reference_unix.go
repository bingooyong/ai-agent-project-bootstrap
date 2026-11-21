//go:build darwin || linux

package safeio

import (
	"errors"
	"path"

	"golang.org/x/sys/unix"
)

// CheckReference checks for a regular file or directory without opening its
// contents or following an intermediate or final symbolic link.
func (r *Root) CheckReference(name string) error {
	if ValidatePath(name) != nil || Sensitive(name) {
		return ErrUnsafeReference
	}
	parent, err := r.open(path.Dir(name), true)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return ErrUnsafeReference
		}
		return err
	}
	defer parent.Close()
	var info unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), path.Base(name), &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	mode := info.Mode & unix.S_IFMT
	if uint64(info.Dev) != r.device || (mode != unix.S_IFREG && mode != unix.S_IFDIR) {
		return ErrUnsafeReference
	}
	return nil
}

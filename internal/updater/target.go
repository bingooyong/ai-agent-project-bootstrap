package updater

import (
	"errors"
	"path/filepath"
)

// ValidateTarget prevents a plan argument from silently choosing another root.
func ValidateTarget(root string, p Plan) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	if abs != p.Root {
		return errors.New("plan root does not match requested target")
	}
	return nil
}

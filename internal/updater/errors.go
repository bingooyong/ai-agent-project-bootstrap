package updater

import "errors"

type operationError struct {
	code  int
	cause error
}

func (e *operationError) Error() string { return e.cause.Error() }
func (e *operationError) Unwrap() error { return e.cause }
func classified(code int, err error) error {
	if err == nil {
		return nil
	}
	return &operationError{code, err}
}

// ExitCode distinguishes invalid configuration, incomplete scanning and safety
// blockers without requiring callers to parse untrusted diagnostic strings.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *operationError
	if errors.As(err, &e) {
		return e.code
	}
	return 4
}

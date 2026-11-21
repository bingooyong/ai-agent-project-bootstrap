package safeio

import "errors"

// ErrUnsafeReference means a metadata lookup was refused by path policy.
var ErrUnsafeReference = errors.New("unsafe reference path")

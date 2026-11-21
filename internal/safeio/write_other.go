//go:build !darwin && !linux

package safeio

import (
	"errors"
	"io/fs"
)

var errWriteUnsupported = errors.New("strict descriptor-relative project writes are currently supported on macOS and Linux only")

func (*Root) MkdirAll(string, fs.FileMode) error       { return errWriteUnsupported }
func (*Root) MkdirExclusive(string, fs.FileMode) error { return errWriteUnsupported }
func (*Root) CheckCreate(string) error                 { return errWriteUnsupported }
func (*Root) Replace(string, []byte, string) error     { return errWriteUnsupported }
func (*Root) RemoveIf(string, string) error            { return errWriteUnsupported }
func (*Root) RemoveEmptyDir(string) error              { return errWriteUnsupported }

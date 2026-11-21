//go:build !darwin && !linux

package safeio

import (
	"errors"
	"io/fs"
)

type Root struct{}

func OpenExternalParent(string) (*Root, string, error) { return nil, "", errUnsupported }

var errUnsupported = errors.New("strict descriptor-relative project reads are currently supported on macOS and Linux only")

func Open(string) (*Root, error)                              { return nil, errUnsupported }
func (*Root) Close() error                                    { return nil }
func (*Root) Read(string, int64) ([]byte, error)              { return nil, errUnsupported }
func (*Root) ReadDir(string) ([]fs.DirEntry, error)           { return nil, errUnsupported }
func (*Root) ReadDirLimit(string, int) ([]fs.DirEntry, error) { return nil, errUnsupported }
func (*Root) Lstat(string) (fs.FileInfo, error)               { return nil, errUnsupported }

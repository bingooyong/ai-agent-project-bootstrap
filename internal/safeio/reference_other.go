//go:build !darwin && !linux

package safeio

func (*Root) CheckReference(string) error { return errUnsupported }

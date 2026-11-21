//go:build !darwin && !linux

package updater

func processAlive(int) (bool, bool) { return false, false }

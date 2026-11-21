// Package safeio provides bounded, descriptor-relative static project reads.
package safeio

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path"
	"strings"
)

// ErrUnsafeOutput means the output path uses a link or a non-directory parent.
var ErrUnsafeOutput = errors.New("unsafe output path")

func Digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

// ValidatePath accepts portable relative paths; '.' denotes the selected root.
func ValidatePath(name string) error {
	if name == "." {
		return nil
	}
	if name == "" || strings.ContainsAny(name, "\\:\x00\r\n") || strings.HasPrefix(name, "/") || path.Clean(name) != name {
		return fmt.Errorf("invalid relative path")
	}
	for _, p := range strings.Split(name, "/") {
		base := strings.ToUpper(strings.Split(p, ".")[0])
		if p == ".." || p == "." || strings.HasSuffix(p, ".") || strings.HasSuffix(p, " ") || base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
			return fmt.Errorf("nonportable relative path")
		}
	}
	return nil
}

func Sensitive(name string) bool {
	for _, part := range strings.Split(strings.ToLower(name), "/") {
		if part == ".ssh" || part == ".aws" || part == ".gnupg" || part == "secrets" || part == "credentials" || part == "credentials.json" || part == "auth.json" || part == ".npmrc" || part == ".pypirc" || part == ".netrc" || part == "id_rsa" || part == "id_ed25519" || part == ".env" || strings.HasPrefix(part, ".env.") {
			return true
		}
		for _, ext := range []string{".pem", ".key", ".p12", ".pfx", ".crt", ".cer", ".keystore"} {
			if strings.HasSuffix(part, ext) {
				return true
			}
		}
	}
	return false
}

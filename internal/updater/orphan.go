package updater

import (
	"encoding/hex"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
	"github.com/bingooyong/oh-my-blueprint/internal/strictio"
)

func validReviewDigest(value string) bool {
	return validHex(value, 32)
}

func validHex(value string, size int) bool {
	if len(value) != size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func requireDeadOwner(b []byte) error {
	var o owner
	if strictio.Decode(b, &o) != nil || o.PID <= 0 || o.PID > 1<<31-1 || !validHex(o.Nonce, 16) {
		return errors.New("write lock owner is unknown; preserve it for inspection")
	}
	alive, known := processAlive(o.PID)
	if !known || alive {
		return errors.New("cannot recover while the write owner is alive or unknown")
	}
	return nil
}

func readOrphanedMutex(r *safeio.Root) ([]byte, error) {
	b, err := r.Read(mutexPath, 4096)
	if err != nil {
		return nil, err
	}
	if err := requireDeadOwner(b); err != nil {
		return nil, err
	}
	if _, err := r.Lstat(journalPath); !errors.Is(err, fs.ErrNotExist) {
		return nil, errors.New("transaction journal exists or cannot be ruled out; review journal recovery")
	}
	return b, nil
}

func orphanReviewDigest(root string, mutex []byte) string {
	return safeio.Digest([]byte("ombp:orphaned-mutex:v1\x00" + root + "\x00" + safeio.Digest(mutex)))
}

// RecoverOrphanedMutex releases only a reviewed dead writer's mutex when
// there is no journal to replay. It never modifies product outputs.
func RecoverOrphanedMutex(root, reviewedSHA256 string) error {
	if !validReviewDigest(reviewedSHA256) {
		return errors.New("recovery requires the reviewed mutex SHA-256")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	r, err := safeio.Open(abs)
	if err != nil {
		return err
	}
	defer r.Close()
	b, err := readOrphanedMutex(r)
	if err != nil {
		return err
	}
	if orphanReviewDigest(abs, b) != reviewedSHA256 {
		return errors.New("write mutex changed after review")
	}
	return r.RemoveIf(mutexPath, safeio.Digest(b))
}

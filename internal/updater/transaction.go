package updater

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
	"github.com/bingooyong/oh-my-blueprint/internal/strictio"
)

const journalPath = ".ombp/state/journal.json"
const mutexPath = ".ombp/state/mutex.json"

type owner struct {
	PID   int    `json:"pid" yaml:"pid"`
	Nonce string `json:"nonce" yaml:"nonce"`
}
type intent struct {
	Path      string `json:"path" yaml:"path"`
	Old       string `json:"old" yaml:"old"`
	OldSHA256 string `json:"old_sha256" yaml:"old_sha256"`
	NewSHA256 string `json:"new_sha256" yaml:"new_sha256"`
}
type journal struct {
	SchemaVersion string   `json:"schema_version" yaml:"schema_version"`
	Root          string   `json:"root" yaml:"root"`
	State         string   `json:"state" yaml:"state"`
	Entries       []intent `json:"entries" yaml:"entries"`
}

func acquire(r *safeio.Root) (func() error, error) {
	if err := r.MkdirAll(".ombp/state", 0700); err != nil {
		return nil, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	b := encode(owner{os.Getpid(), hex.EncodeToString(nonce[:])})
	if err := r.Replace(mutexPath, b, ""); err != nil {
		return nil, errors.New("project write mutex already exists or could not be created")
	}
	return func() error { return r.RemoveIf(mutexPath, safeio.Digest(b)) }, nil
}

// Apply treats the plan as untrusted. Fresh resolution must produce exactly the
// same root, sources and outputs before any product output is changed.
func Apply(ctx context.Context, p Plan, version string) error { return apply(ctx, p, version, nil) }

func apply(ctx context.Context, p Plan, version string, hook func(string, string)) (result error) {
	if p.SchemaVersion != "1.0" || p.ToolVersion != version || ValidateConfigPath(p.ConfigPath) != nil || planDigest(p) != p.Fingerprint {
		return errors.New("plan protocol or fingerprint mismatch")
	}
	r, err := safeio.Open(p.Root)
	if err != nil {
		return err
	}
	defer r.Close()
	release, err := acquire(r)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, release()) }()
	if hook != nil {
		hook("mutex-acquired", "")
	}
	fresh, err := BuildWithConfig(ctx, p.Root, version, p.ConfigPath)
	if err != nil {
		return err
	}
	if fresh.Fingerprint != p.Fingerprint {
		return errors.New("plan is stale; create a new preview")
	}
	if len(p.Changes) == 0 {
		return nil
	}
	j := journal{"1.0", p.Root, "pending", []intent{}}
	for _, change := range fresh.Changes {
		old, err := readOptional(r, change.Path)
		if err != nil {
			return err
		}
		oldHash := ""
		if old != nil {
			oldHash = safeio.Digest(old)
		}
		if oldHash != change.BeforeSHA256 {
			return errors.New("target changed before journal creation")
		}
		j.Entries = append(j.Entries, intent{change.Path, string(old), oldHash, change.AfterSHA256})
	}
	data := encode(j)
	if len(data) > 4<<20 {
		return errors.New("transaction journal exceeds budget")
	}
	if err := r.Replace(journalPath, data, ""); err != nil {
		return err
	}
	if hook != nil {
		hook("intent", "")
	}
	fail := func(cause error) error {
		if recoveryErr := rollback(r, j); recoveryErr != nil {
			return fmt.Errorf("write failed; recovery requires inspection: %w", errors.Join(cause, recoveryErr))
		}
		if e := r.RemoveIf(journalPath, safeio.Digest(data)); e != nil {
			return errors.Join(cause, e)
		}
		return cause
	}
	for _, change := range fresh.Changes {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if err := r.MkdirAll(path.Dir(change.Path), 0700); err != nil {
			return fail(err)
		}
		if hook != nil {
			hook("before-replace", change.Path)
		}
		if err := r.Replace(change.Path, []byte(change.Content), change.BeforeSHA256); err != nil {
			return fail(err)
		}
		if hook != nil {
			hook("after-replace", change.Path)
		}
	}
	for _, change := range fresh.Changes {
		b, e := r.Read(change.Path, 4<<20)
		if e != nil || safeio.Digest(b) != change.AfterSHA256 {
			return fail(errors.New("post-write verification failed"))
		}
	}
	if hook != nil {
		hook("before-commit", "")
	}
	j.State = "committed"
	committed := encode(j)
	if err := r.Replace(journalPath, committed, safeio.Digest(data)); err != nil {
		// It may already have committed before a directory fsync error. Keep the
		// journal for Recover rather than assuming no mutation occurred.
		return fmt.Errorf("commit marker could not be confirmed: %w", err)
	}
	if hook != nil {
		hook("committed", "")
	}
	if err := r.RemoveIf(journalPath, safeio.Digest(committed)); err != nil {
		return err
	}
	if hook != nil {
		hook("journal-removed", "")
	}
	return nil
}

func allowedOutput(name string) bool {
	if safeio.ValidatePath(name) != nil || safeio.Sensitive(name) {
		return false
	}
	return name == DefaultConfigPath || name == profilePath || name == lockPath || path.Base(name) == "AGENTS.md" && !strings.HasPrefix(name, ".ombp/")
}

func validateJournal(j journal, root string) error {
	if j.SchemaVersion != "1.0" || j.Root != root || (j.State != "pending" && j.State != "committed") || len(j.Entries) > 128 || len(j.Entries) == 0 {
		return errors.New("invalid recovery journal")
	}
	seen := map[string]bool{}
	for _, e := range j.Entries {
		if !allowedOutput(e.Path) || seen[e.Path] || len(e.NewSHA256) != 64 || (e.OldSHA256 != "" && safeio.Digest([]byte(e.Old)) != e.OldSHA256) || (e.OldSHA256 == "" && e.Old != "") {
			return errors.New("invalid recovery intent")
		}
		seen[e.Path] = true
	}
	return nil
}

func decodeJournal(data []byte, root string) (journal, error) {
	var j journal
	if strictio.Decode(data, &j) != nil {
		return j, errors.New("invalid recovery journal")
	}
	return j, validateJournal(j, root)
}

func rollback(r *safeio.Root, j journal) error {
	// Preflight every intent, including writes interrupted before a completion
	// flag could be recorded. If any user edit differs, preserve all remaining
	// files and leave the journal for explicit inspection.
	for _, e := range j.Entries {
		current, err := readOptional(r, e.Path)
		if err != nil {
			return err
		}
		hash := ""
		if current != nil {
			hash = safeio.Digest(current)
		}
		if hash != e.OldSHA256 && hash != e.NewSHA256 {
			return fmt.Errorf("recovery preserves a concurrent edit: %s", e.Path)
		}
	}
	for i := len(j.Entries) - 1; i >= 0; i-- {
		e := j.Entries[i]
		current, err := readOptional(r, e.Path)
		if err != nil {
			return err
		}
		hash := ""
		if current != nil {
			hash = safeio.Digest(current)
		}
		if hash == e.OldSHA256 {
			continue
		}
		if e.OldSHA256 == "" {
			err = r.RemoveIf(e.Path, e.NewSHA256)
		} else {
			err = r.Replace(e.Path, []byte(e.Old), e.NewSHA256)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

type RecoveryReview struct {
	JournalSHA256 string          `json:"journal_sha256,omitempty"`
	MutexSHA256   string          `json:"mutex_sha256,omitempty"`
	State         string          `json:"state"`
	Entries       []RecoveryEntry `json:"entries"`
}
type RecoveryEntry struct {
	Path                  string `json:"path"`
	Old                   string `json:"old_content"`
	Current               string `json:"current_content"`
	ExpectedCurrentSHA256 string `json:"new_sha256"`
}

// ReviewRecovery is read-only. Repository state is not an authenticated command:
// callers must review the displayed conditional restoration before applying it.
func ReviewRecovery(root string) (RecoveryReview, error) {
	abs, e := filepath.Abs(root)
	if e != nil {
		return RecoveryReview{}, e
	}
	abs, e = filepath.EvalSymlinks(abs)
	if e != nil {
		return RecoveryReview{}, e
	}
	r, e := safeio.Open(abs)
	if e != nil {
		return RecoveryReview{}, e
	}
	defer r.Close()
	b, e := r.Read(journalPath, 4<<20)
	if errors.Is(e, fs.ErrNotExist) {
		mutex, err := readOrphanedMutex(r)
		if err != nil {
			return RecoveryReview{}, err
		}
		return RecoveryReview{MutexSHA256: orphanReviewDigest(abs, mutex), State: "orphaned_mutex", Entries: []RecoveryEntry{}}, nil
	}
	if e != nil {
		return RecoveryReview{}, e
	}
	j, e := decodeJournal(b, abs)
	if e != nil {
		return RecoveryReview{}, e
	}
	review := RecoveryReview{JournalSHA256: safeio.Digest(b), State: j.State, Entries: []RecoveryEntry{}}
	for _, entry := range j.Entries {
		current, e := readOptional(r, entry.Path)
		if e != nil {
			return RecoveryReview{}, e
		}
		review.Entries = append(review.Entries, RecoveryEntry{entry.Path, entry.Old, string(current), entry.NewSHA256})
	}
	return review, nil
}

// Recover applies a specifically reviewed journal, never an arbitrary new state
// file. A live/unknown lock owner prevents recovery; hashes protect user edits.
func Recover(root string, reviewedSHA256 string) (result error) {
	if !validReviewDigest(reviewedSHA256) {
		return errors.New("recovery requires the reviewed journal SHA-256")
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
	before, e := r.Read(journalPath, 4<<20)
	if e != nil {
		return e
	}
	if safeio.Digest(before) != reviewedSHA256 {
		return errors.New("recovery journal changed after review")
	}
	if _, err := decodeJournal(before, abs); err != nil {
		return err
	}
	if b, e := readOptional(r, mutexPath); e != nil {
		return e
	} else if b != nil {
		if err := requireDeadOwner(b); err != nil {
			return err
		}
		if err := r.RemoveIf(mutexPath, safeio.Digest(b)); err != nil {
			return err
		}
	}
	release, err := acquire(r)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, release()) }()
	b, err := readOptional(r, journalPath)
	if err != nil {
		return err
	}
	if b == nil {
		return errors.New("reviewed recovery journal is no longer present")
	}
	if safeio.Digest(b) != reviewedSHA256 {
		return errors.New("recovery journal changed after review")
	}
	j, err := decodeJournal(b, abs)
	if err != nil {
		return err
	}
	if j.State == "committed" {
		for _, e := range j.Entries {
			current, err := r.Read(e.Path, 4<<20)
			if err != nil || safeio.Digest(current) != e.NewSHA256 {
				return errors.New("committed output changed; preserve recovery journal")
			}
		}
	} else if err := rollback(r, j); err != nil {
		return err
	}
	return r.RemoveIf(journalPath, safeio.Digest(b))
}

// SavePlan explicitly creates a caller-selected file and never overwrites it.
// It has the same no-follow protection as project I/O, even outside the target.
func SavePlan(name string, p Plan) error {
	r, base, err := safeio.OpenExternalParent(name)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Replace(base, encode(p), "")
}

func ReadPlan(name string) (Plan, error) {
	r, base, err := safeio.OpenExternalParent(name)
	if err != nil {
		return Plan{}, err
	}
	defer r.Close()
	b, err := r.Read(base, 4<<20)
	if err != nil {
		return Plan{}, err
	}
	return ParsePlan(b)
}

// Keep fs imported to make absence behavior explicit at the transaction edge.
var _ = fs.ErrNotExist

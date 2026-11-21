//go:build darwin || linux

package updater

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
)

func TestRecoverEveryIntentWindow(t *testing.T) {
	for _, point := range []string{"intent", "before-replace", "after-replace", "before-commit", "committed"} {
		t.Run(point, func(t *testing.T) {
			r := fixture(t)
			p := build(t, r)
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("interruption hook did not run")
					}
				}()
				_ = apply(context.Background(), p, testVersion, func(phase, _ string) {
					if phase == point {
						panic("simulated process interruption")
					}
				})
			}()
			review, e := ReviewRecovery(r)
			if e != nil {
				t.Fatal(e)
			}
			if e = Recover(r, review.JournalSHA256); e != nil {
				t.Fatal(e)
			}
			if _, e = os.Stat(filepath.Join(r, journalPath)); !os.IsNotExist(e) {
				t.Fatal("journal not cleared")
			}
			if point == "committed" {
				if len(build(t, r).Changes) != 0 {
					t.Fatal("committed recovery lost outputs")
				}
			} else {
				if _, e = os.Stat(filepath.Join(r, "AGENTS.md")); !os.IsNotExist(e) {
					t.Fatal("pending rollback did not remove new output")
				}
			}
			if _, e = ReviewRecovery(r); e == nil {
				t.Fatal("recovery did not converge")
			}
		})
	}
}

func TestRecoveryPreservesConcurrentEditAndRequiresReview(t *testing.T) {
	r := fixture(t)
	p := build(t, r)
	func() {
		defer func() { _ = recover() }()
		_ = apply(context.Background(), p, testVersion, func(phase, name string) {
			if phase == "after-replace" && name == "AGENTS.md" {
				panic("interrupted")
			}
		})
	}()
	review, e := ReviewRecovery(r)
	if e != nil {
		t.Fatal(e)
	}
	if e = Recover(r, ""); e == nil {
		t.Fatal("unreviewed recovery accepted")
	}
	write(t, r, "AGENTS.md", "manual concurrent change")
	if e = Recover(r, review.JournalSHA256); e == nil {
		t.Fatal("overwrote concurrent edit")
	}
	if read(t, r, "AGENTS.md") != "manual concurrent change" {
		t.Fatal("manual content lost")
	}
	if _, e = os.Stat(filepath.Join(r, journalPath)); e != nil {
		t.Fatal("recovery evidence removed")
	}
}

func TestLiveWriteOwnerIsNotRemoved(t *testing.T) {
	r := fixture(t)
	root, e := safeio.Open(r)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	release, e := acquire(root)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	if _, e = ReviewRecovery(r); e == nil {
		t.Fatal("live writer without a journal was offered orphan recovery")
	}
	if e = RecoverOrphanedMutex(r, safeio.Digest([]byte(read(t, r, mutexPath)))); e == nil {
		t.Fatal("removed a live writer without a journal")
	}
	if e = Apply(context.Background(), build(t, r), testVersion); e == nil {
		t.Fatal("concurrent owner bypassed")
	}
	if _, e = os.Stat(filepath.Join(r, mutexPath)); e != nil {
		t.Fatal("live mutex removed")
	}
}

func TestCrashProcessHelper(t *testing.T) {
	if os.Getenv("OMBP_TEST_CRASH_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	r := os.Getenv("OMBP_TEST_ROOT")
	point := os.Getenv("OMBP_TEST_CRASH_POINT")
	p, e := Build(context.Background(), r, testVersion)
	if e != nil {
		t.Fatal(e)
	}
	_ = apply(context.Background(), p, testVersion, func(phase, name string) {
		if phase == point || point == "after-lock" && phase == "after-replace" && name == lockPath {
			os.Exit(91)
		}
	})
	t.Fatal("expected process to exit at crash point")
}

func TestProcessCrashRecovery(t *testing.T) {
	for _, point := range []string{"mutex-acquired", "intent", "after-replace", "after-lock", "before-commit", "committed", "journal-removed"} {
		t.Run(point, func(t *testing.T) {
			r := fixture(t)
			cmd := exec.Command(os.Args[0], "-test.run=^TestCrashProcessHelper$")
			cmd.Env = append(os.Environ(), "OMBP_TEST_CRASH_HELPER=1", "OMBP_TEST_ROOT="+r, "OMBP_TEST_CRASH_POINT="+point)
			output, e := cmd.CombinedOutput()
			exit, ok := e.(*exec.ExitError)
			if !ok || exit.ExitCode() != 91 {
				t.Fatalf("helper %v: %s", e, output)
			}
			if _, e = os.Stat(filepath.Join(r, mutexPath)); e != nil {
				t.Fatal("real crash did not retain owner record")
			}
			review, e := ReviewRecovery(r)
			if e != nil {
				t.Fatal(e)
			}
			if point == "mutex-acquired" || point == "journal-removed" {
				if review.State != "orphaned_mutex" || review.JournalSHA256 != "" || review.MutexSHA256 == "" || len(review.Entries) != 0 {
					t.Fatalf("unexpected orphan review: %#v", review)
				}
				if e = Recover(r, review.MutexSHA256); e == nil {
					t.Fatal("orphan digest was accepted as a journal digest")
				}
				if e = RecoverOrphanedMutex(r, safeio.Digest([]byte("different lock"))); e == nil {
					t.Fatal("accepted unreviewed mutex")
				}
				other := fixture(t)
				write(t, other, mutexPath, read(t, r, mutexPath))
				if e = RecoverOrphanedMutex(other, review.MutexSHA256); e == nil {
					t.Fatal("accepted orphan review for a different project with the same mutex bytes")
				}
				const wrongJournal = `{"schema_version":"1.0","root":"/different-project","state":"pending","entries":[]}`
				write(t, r, journalPath, wrongJournal)
				if e = RecoverOrphanedMutex(r, review.MutexSHA256); e == nil {
					t.Fatal("removed ownership while a new journal was present")
				}
				oldMutex := read(t, r, mutexPath)
				if e = Recover(r, safeio.Digest([]byte(wrongJournal))); e == nil || read(t, r, mutexPath) != oldMutex {
					t.Fatal("invalid journal was accepted or its validation modified the mutex")
				}
				if e = os.Remove(filepath.Join(r, journalPath)); e != nil {
					t.Fatal(e)
				}
				if e = RecoverOrphanedMutex(r, review.MutexSHA256); e != nil {
					t.Fatal(e)
				}
			} else if e = Recover(r, review.JournalSHA256); e != nil {
				t.Fatal(e)
			}
			if _, e = os.Stat(filepath.Join(r, mutexPath)); !os.IsNotExist(e) {
				t.Fatal("dead writer mutex not released")
			}
			if point == "committed" || point == "journal-removed" {
				if len(build(t, r).Changes) != 0 {
					t.Fatal("committed crash recovery changed outputs")
				}
			} else {
				if _, e = os.Stat(filepath.Join(r, "AGENTS.md")); !os.IsNotExist(e) {
					t.Fatal("uncommitted output survived rollback")
				}
			}
		})
	}
}

func TestInvalidOwnerCannotBeRecovered(t *testing.T) {
	r := fixture(t)
	const malformed = `{"pid":2147483648,"nonce":"00000000000000000000000000000000"}`
	write(t, r, mutexPath, malformed)
	if _, err := ReviewRecovery(r); err == nil {
		t.Fatal("out-of-range owner is reviewable")
	}
	if err := RecoverOrphanedMutex(r, safeio.Digest([]byte(malformed))); err == nil {
		t.Fatal("removed unrecognized owner")
	}
	if read(t, r, mutexPath) != malformed {
		t.Fatal("invalid mutex was changed")
	}
}

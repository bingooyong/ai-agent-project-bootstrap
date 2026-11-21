package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
	"github.com/bingooyong/oh-my-blueprint/internal/updater"
)

func TestReportOutputFormatAndScope(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			project := t.TempDir()
			args := []string{"scan", project, "--ci", "--format", format, "--output", "report." + format}
			out := runReportCommand(t, 0, args...)
			name := filepath.Join(project, "report."+format)
			saved, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(saved, out) {
				t.Fatalf("saved %s does not match stdout", format)
			}
			info, err := os.Stat(name)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm()&0077 != 0 {
				t.Fatalf("report permissions are %v", info.Mode().Perm())
			}
			assertDirectoryNames(t, project, []string{"report." + format})
			runReportCommand(t, 3, args...)
			after, err := os.ReadFile(name)
			if err != nil || !bytes.Equal(after, saved) {
				t.Fatalf("existing report changed, err=%v", err)
			}
		})
	}
}

func TestReportOutputRejectsProtectedDestinations(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
	}{
		{"empty", ""},
		{"project root", "."},
		{"case alias of agent instructions", "agents.override.MD"},
		{"case alias of OMBP state", ".OMBP/lock.yaml"},
		{"nested OMBP state", "nested/.ombp/project.yaml"},
		{"project manifest", "package.json"},
		{"agent settings", ".claude/settings.json"},
		{"Git metadata", ".git/config"},
		{"CI workflow", ".github/workflows/check.yml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := t.TempDir()
			runReportCommand(t, 2, "scan", project, "--output", tc.path)
			assertDirectoryNames(t, project, []string{})
		})
	}
	project := t.TempDir()
	runReportCommand(t, 2, "scan", project, "--config", "blueprint.yaml", "--output", "blueprint.yaml")
	assertDirectoryNames(t, project, []string{})
	runReportCommand(t, 3, "scan", project, "--output", "missing/report.json")
	assertDirectoryNames(t, project, []string{})
}

func TestReportOutputRejectsLinksAndKeepsFindings(t *testing.T) {
	project, external := t.TempDir(), t.TempDir()
	if err := os.Symlink(external, filepath.Join(project, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "report.json"), filepath.Join(project, "report.json")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"link/report.json", "report.json"} {
		t.Run(name, func(t *testing.T) {
			out := runReportCommand(t, 4, "check", project, "--output", name, "--format", "json")
			var result Report
			if err := json.Unmarshal(out, &result); err != nil {
				t.Fatal(err)
			}
			codes := map[string]bool{}
			for _, f := range result.Findings {
				codes[f.Code] = true
			}
			if result.ExitCode != 4 || result.Result != "blocked" || !codes["LOCK_MISSING"] || !codes["REPORT_WRITE_FAILED"] {
				t.Fatalf("report lost inspection or write findings: %s", out)
			}
		})
	}
	assertDirectoryNames(t, external, []string{})
}

func TestReportOutputDoesNotChangePlanAuthority(t *testing.T) {
	project := t.TempDir()
	plan := filepath.Join(t.TempDir(), "approved.json")
	runReportCommand(t, 0, "diff", project, "--output", "preview.json", "--format", "json", "--plan-out", plan)
	assertDirectoryNames(t, project, []string{"preview.json"})
	preview, err := os.ReadFile(filepath.Join(project, "preview.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Data struct {
			Fingerprint string            `json:"plan_fingerprint"`
			Changes     []json.RawMessage `json:"changes"`
		}
	}
	if err := json.Unmarshal(preview, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Data.Fingerprint == "" || len(saved.Data.Changes) == 0 || bytes.Contains(preview, []byte(project)) ||
		bytes.Contains(preview, []byte(`"content"`)) || bytes.Contains(preview, []byte(`"diff"`)) {
		t.Fatalf("report should contain plan metadata without machine-local content: %s", preview)
	}
	runReportCommand(t, 2, "init", project, "--ci", "--apply", "--plan", plan, "--output", "preview.json")
	runReportCommand(t, 3, "init", project, "--apply", "--plan", plan, "--output", "preview.json")
	assertDirectoryNames(t, project, []string{"preview.json"})
	collision := filepath.Join(project, "both.json")
	runReportCommand(t, 2, "diff", project, "--output", "both.json", "--plan-out", collision)
	if _, err := os.Stat(collision); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("conflicting destination should be absent, err=%v", err)
	}
}

func TestReportRecordsOperationFailure(t *testing.T) {
	project := t.TempDir()
	out := runReportCommand(t, 4, "recover", project, "--format", "json", "--output", "blocked.json")
	saved, err := os.ReadFile(filepath.Join(project, "blocked.json"))
	if err != nil || !bytes.Equal(saved, out) {
		t.Fatalf("blocked report does not match stdout, err=%v", err)
	}
	var result Report
	if err := json.Unmarshal(saved, &result); err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 4 || result.Result != "blocked" {
		t.Fatalf("unexpected report: %s", saved)
	}
}

func TestSavedRecoveryReviewOmitsContents(t *testing.T) {
	project := t.TempDir()
	root, err := safeio.Open(project)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	r := Report{Data: updater.RecoveryReview{
		JournalSHA256: "review-fingerprint",
		State:         "pending",
		Entries: []updater.RecoveryEntry{{
			Path: "AGENTS.md", Old: "old-file-sentinel", Current: "new-file-sentinel",
		}},
	}}
	if err := saveReport(root, "recovery.json", "json", r); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(project, "recovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(saved, []byte("review-fingerprint")) || !bytes.Contains(saved, []byte("AGENTS.md")) || bytes.Contains(saved, []byte("file-sentinel")) {
		t.Fatalf("unexpected recovery summary: %s", saved)
	}
}

func runReportCommand(t *testing.T, want int, args ...string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), args, &stdout, &stderr); code != want {
		t.Fatalf("%v returned %d, want %d, stderr=%q, stdout=%q", args, code, want, stderr.String(), stdout.String())
	}
	return stdout.Bytes()
}

func assertDirectoryNames(t *testing.T, dir string, want []string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("directory contents=%v, want %v", names, want)
	}
}

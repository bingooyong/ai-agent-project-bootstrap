//go:build darwin || linux

package governance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
	"github.com/bingooyong/oh-my-blueprint/internal/updater"
)

const version = "test-1"

func initialize(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/fixture\n\ngo 1.25.0\n"), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := updater.Build(context.Background(), root, version)
	if e != nil {
		t.Fatal(e)
	}
	if e = updater.Apply(context.Background(), p, version); e != nil {
		t.Fatal(e)
	}
	return root
}
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if e := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			out[p] = safeio.Digest(b)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	return out
}
func has(r Report, code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestCheckCleanIsReadOnly(t *testing.T) {
	r := initialize(t)
	before := snapshot(t, r)
	result, e := Check(context.Background(), r, version)
	if e != nil || result.ExitCode != 0 {
		t.Fatalf("%v %#v", e, result)
	}
	after := snapshot(t, r)
	if len(before) != len(after) {
		t.Fatal("check created files")
	}
	for p, h := range before {
		if after[p] != h {
			t.Fatalf("check changed %s", p)
		}
	}
}

func TestCheckReportsSourcesAndProtectsManagedText(t *testing.T) {
	for _, tc := range []struct {
		name, file, value, code string
		exit                    int
	}{
		{"source", "go.mod", "module example.test/changed\n\ngo 1.25.0\n", "PROJECT_SOURCE_DRIFT", 1},
		{"managed", "AGENTS.md", "manual replacement", "MANAGED_OUTPUT_CHANGED", 4},
		{"profile", ".ombp/profile.json", "{}", "PROFILE_INVALID", 4},
		{"config", ".ombp/project.yaml", "{}", "CONFIG_INVALID", 2},
		{"override", "AGENTS.override.md", "override", "OVERRIDE_PRECEDENCE", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := initialize(t)
			if e := os.WriteFile(filepath.Join(r, tc.file), []byte(tc.value), 0600); e != nil {
				t.Fatal(e)
			}
			result, e := Check(context.Background(), r, version)
			if e != nil || result.ExitCode != tc.exit || !has(result, tc.code) {
				t.Fatalf("%v %#v", e, result)
			}
		})
	}
}

func TestManualAreaIsPreservedButBudgetChecked(t *testing.T) {
	r := initialize(t)
	name := filepath.Join(r, "AGENTS.md")
	b, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(name, append(b, []byte("\nmanual note\n")...), 0600); e != nil {
		t.Fatal(e)
	}
	result, e := Check(context.Background(), r, version)
	if e != nil || result.ExitCode != 0 || !has(result, "MANUAL_TEXT_CHANGED") {
		t.Fatalf("%v %#v", e, result)
	}
	if e = os.WriteFile(name, append(b, []byte(strings.Repeat("x", 32768))...), 0600); e != nil {
		t.Fatal(e)
	}
	result, e = Check(context.Background(), r, version)
	if e != nil || result.ExitCode != 4 || !has(result, "INSTRUCTION_BUDGET_EXCEEDED") {
		t.Fatalf("%v %#v", e, result)
	}
}

func TestUnknownAndIncompleteAreNotClean(t *testing.T) {
	r := t.TempDir()
	result, e := Check(context.Background(), r, version)
	if e != nil || result.ExitCode != 1 || !has(result, "PROJECT_FACT_UNKNOWN") {
		t.Fatalf("%v %#v", e, result)
	}
	if e = os.WriteFile(filepath.Join(r, "package.json"), []byte("{"), 0600); e != nil {
		t.Fatal(e)
	}
	result, e = Check(context.Background(), r, version)
	if e == nil || result.ExitCode != 3 || result.Coverage.Status != "incomplete" {
		t.Fatalf("%v %#v", e, result)
	}
}

func TestForgedEditableLockCannotHideMissingRules(t *testing.T) {
	r := initialize(t)
	lockPath := filepath.Join(r, ".ombp/lock.yaml")
	data, e := os.ReadFile(lockPath)
	if e != nil {
		t.Fatal(e)
	}
	var lock updater.Lock
	if e = json.Unmarshal(data, &lock); e != nil {
		t.Fatal(e)
	}
	content := "<!-- ombp:begin id=project-core schema=1 -->\n# Forged instructions\n<!-- ombp:end id=project-core -->\n"
	block, e := updater.ManagedBlock(content)
	if e != nil {
		t.Fatal(e)
	}
	lock.Outputs["AGENTS.md"] = updater.Output{SHA256: safeio.Digest([]byte(content)), BlockSHA256: safeio.Digest([]byte(block))}
	lock.Rules = []updater.RuleLock{}
	data, e = json.Marshal(lock)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(lockPath, data, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(r, "AGENTS.md"), []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
	result, e := Check(context.Background(), r, version)
	if e != nil || result.ExitCode != 4 || !has(result, "RULE_INVENTORY_MISMATCH") || !has(result, "REQUIRED_RULE_CONTENT_MISMATCH") {
		t.Fatalf("%v %#v", e, result)
	}
	if _, e = updater.Build(context.Background(), r, version); e == nil {
		t.Fatal("Build accepted forged rule inventory")
	}
}

func TestCheckCountsUnmanagedIntermediateAncestors(t *testing.T) {
	r := t.TempDir()
	for _, file := range []string{"go.mod", "a/b/go.mod"} {
		name := filepath.Join(r, file)
		if e := os.MkdirAll(filepath.Dir(name), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(name, []byte("module example.test/fixture\n\ngo 1.25.0\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	ancestor := filepath.Join(r, "a/AGENTS.md")
	if e := os.WriteFile(ancestor, []byte("manual ancestor"), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := updater.Build(context.Background(), r, version)
	if e != nil {
		t.Fatal(e)
	}
	if e = updater.Apply(context.Background(), p, version); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(ancestor, []byte(strings.Repeat("x", 40000)), 0600); e != nil {
		t.Fatal(e)
	}
	result, e := Check(context.Background(), r, version)
	if e != nil || result.ExitCode != 4 || !has(result, "INSTRUCTION_BUDGET_EXCEEDED") {
		t.Fatalf("%v %#v", e, result)
	}
}

func TestCheckLocatesCommandSourceChanges(t *testing.T) {
	const initial = `{"packageManager":"npm@10.0.0","scripts":{"test":"fixture-script-body"}}`
	for _, tc := range []struct {
		name, change, wantCode string
		wantExit               int
	}{
		{"removed script", `{"scripts":{}}`, "COMMAND_SOURCE_REMOVED", 1},
		{"npm to pnpm", `{"packageManager":"pnpm@10.0.0","scripts":{"test":"fixture-script-body"}}`, "COMMAND_SOURCE_DRIFT", 1},
		{"unrelated edit", `{"description":"new","scripts":{"test":"fixture-script-body"}}`, "", 1},
		{"unknown package manager", `{"packageManager":"unknown@1","scripts":{"test":"fixture-script-body"}}`, "COMMAND_SOURCE_UNVERIFIED", 1},
		{"malformed manifest", `{"scripts":`, "", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, "package.json", initial)
			applyFixture(t, root)
			writeFixture(t, root, "package.json", tc.change)
			report, err := Check(context.Background(), root, version)
			if report.ExitCode != tc.wantExit || (err != nil) != (tc.wantExit == 3) {
				t.Fatalf("check returned exit %d, error %v, findings %#v", report.ExitCode, err, report.Findings)
			}
			assertCommandFinding(t, report, tc.wantCode, Location{Path: "package.json", Pointer: "/scripts/test"})
		})
	}
}

func TestCheckCommandSourceMovedAndExcluded(t *testing.T) {
	t.Run("same invocation at new location", func(t *testing.T) {
		root := t.TempDir()
		writeFixture(t, root, "Makefile", "test:\n\t@echo fixture-script-body\n")
		applyFixture(t, root)
		if err := os.Rename(filepath.Join(root, "Makefile"), filepath.Join(root, "GNUmakefile")); err != nil {
			t.Fatal(err)
		}
		report, err := Check(context.Background(), root, version)
		if err != nil || report.ExitCode != 1 {
			t.Fatalf("check returned exit %d, error %v", report.ExitCode, err)
		}
		assertCommandFinding(t, report, "COMMAND_SOURCE_DRIFT", Location{Path: "Makefile", Pointer: "line:1"})
	})

	t.Run("symlink replacing source directory", func(t *testing.T) {
		root, outside := t.TempDir(), t.TempDir()
		const script = `{"scripts":{"test":"fixture-script-body"}}`
		writeFixture(t, root, "nested/package.json", script)
		applyFixture(t, root)
		if err := os.Rename(filepath.Join(root, "nested"), filepath.Join(outside, "nested")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(outside, "nested"), filepath.Join(root, "nested")); err != nil {
			t.Fatal(err)
		}
		report, err := Check(context.Background(), root, version)
		if err != nil || report.ExitCode == 0 {
			t.Fatalf("check returned exit %d, error %v", report.ExitCode, err)
		}
		assertCommandFinding(t, report, "COMMAND_SOURCE_UNVERIFIED", Location{Path: "nested/package.json", Pointer: "/scripts/test"})
	})
}

func assertCommandFinding(t *testing.T, report Report, wantCode string, wantLocation Location) {
	t.Helper()
	count := 0
	for _, finding := range report.Findings {
		if !strings.HasPrefix(finding.Code, "COMMAND_SOURCE_") {
			continue
		}
		count++
		if finding.Code != wantCode || finding.Location != wantLocation ||
			finding.Blocking || !finding.RequiresConfirmation ||
			finding.Method != "static" || !strings.Contains(finding.Evidence, `"test"`) ||
			strings.Contains(finding.Evidence, "fixture-script-body") ||
			len(finding.RuleIDs) != 1 || finding.RuleIDs[0] != "commands.source-traceable" {
			t.Fatalf("wrong command diagnostic: %#v", finding)
		}
	}
	if wantCode != "" && count != 1 {
		t.Fatalf("want one %s finding, got %d in %#v", wantCode, count, report.Findings)
	}
}

func writeFixture(t *testing.T, root, name, data string) {
	t.Helper()
	filename := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func applyFixture(t *testing.T, root string) {
	t.Helper()
	ctx := context.Background()
	plan, err := updater.Build(ctx, root, version)
	if err != nil {
		t.Fatal(err)
	}
	if err := updater.Apply(ctx, plan, version); err != nil {
		t.Fatal(err)
	}
}

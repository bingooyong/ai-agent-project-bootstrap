package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProtocol(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"version", "--format", "json"}, 0},
		{[]string{"version", "unexpected"}, 2},
		{[]string{"version", "--format", "yaml"}, 2},
		{[]string{"unknown"}, 2},
		{[]string{"scan", t.TempDir(), "--format", "json"}, 0},
	} {
		var out, errout bytes.Buffer
		code := Run(context.Background(), tc.args, &out, &errout)
		if code != tc.code {
			t.Errorf("%v code=%d want=%d stderr=%s", tc.args, code, tc.code, errout.String())
		}
		if code == 0 {
			var v Report
			if err := json.Unmarshal(out.Bytes(), &v); err != nil {
				t.Fatalf("invalid JSON: %v %s", err, out.String())
			}
			if v.CommandExecution != "NOT_RUN" {
				t.Fatal("manufactured execution")
			}
		}
	}
}

func TestScanDoesNotExecuteScripts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"test":"touch SHOULD_NOT_EXIST"},"dependencies":{"react":"1"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := Run(context.Background(), []string{"scan", dir, "--format", "json"}, &out, &errout); code != 0 {
		t.Fatalf("code=%d %s", code, errout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "SHOULD_NOT_EXIST")); !os.IsNotExist(err) {
		t.Fatal("project script executed")
	}
	if bytes.Contains(out.Bytes(), []byte("touch SHOULD_NOT_EXIST")) {
		t.Fatal("script content echoed")
	}
}

func TestProfileRejectsIncompleteStoredProfiles(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "missing-required-fields",
			body: `{"schema_version":"1.0"}`,
		},
		{
			name: "missing-required-nested-field",
			body: `{"schema_version":"1.0","facts":[],"commands":[],"modules":[],"agent_configs":[],"unknown_items":[],"inputs":{},"coverage":{"files_visited":0,"bytes_read":0,"excluded":[],"counters_omitted":true}}`,
		},
		{
			name: "incomplete-coverage",
			body: `{"schema_version":"1.0","facts":[],"commands":[],"modules":[],"agent_configs":[],"unknown_items":[],"inputs":{},"coverage":{"status":"incomplete","files_visited":0,"bytes_read":0,"excluded":[],"counters_omitted":true}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".ombp"), 0700); err != nil {
				t.Fatal(err)
			}
			profilePath := filepath.Join(root, ".ombp", "profile.json")
			if err := os.WriteFile(profilePath, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), []string{"profile", root, "--format", "json"}, &stdout, &stderr); code != 2 {
				t.Fatalf("accepted invalid stored profile: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestInitPreviewApplyAndTargetBinding(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(t.TempDir(), "reviewed-plan.json")
	run := func(want int, args ...string) []byte {
		t.Helper()
		var out, errout bytes.Buffer
		code := Run(context.Background(), append(args, "--format", "json"), &out, &errout)
		if code != want {
			t.Fatalf("%v: code=%d want=%d stderr=%s stdout=%s", args, code, want, errout.String(), out.String())
		}
		var r Report
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatalf("invalid result %s: %v", out.String(), err)
		}
		if r.ExitCode != want || r.CommandExecution != "NOT_RUN" {
			t.Fatalf("invalid result %#v", r)
		}
		return append([]byte{}, out.Bytes()...)
	}
	run(0, "init", dir, "--dry-run", "--print-diff")
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("dry-run wrote project files")
	}
	run(2, "init", dir, "--dry-run", "--plan-out", plan)
	run(0, "init", dir, "--plan-out", plan)
	run(4, "init", t.TempDir(), "--apply", "--plan", plan)
	run(2, "init", dir, "--apply", "--plan", plan, "--ci")
	run(0, "init", dir, "--apply", "--plan", plan)
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	run(0, "profile", dir)
	run(0, "init", dir, "--dry-run")
}

func TestUnknownArgumentsDoNotLeak(t *testing.T) {
	var out, errout bytes.Buffer
	secret := "fake-credential-sentinel"
	if Run(context.Background(), []string{"version", "--unknown=" + secret, "--format", "json"}, &out, &errout) != 2 {
		t.Fatal("unknown flag accepted")
	}
	if bytes.Contains(out.Bytes(), []byte(secret)) || bytes.Contains(errout.Bytes(), []byte(secret)) {
		t.Fatal("untrusted argument echoed")
	}
}

func TestExplicitReportOutputIsRestricted(t *testing.T) {
	r := t.TempDir()
	call := func(want int, args ...string) {
		t.Helper()
		var out, errout bytes.Buffer
		code := Run(context.Background(), append(args, "--format", "json"), &out, &errout)
		if code != want {
			t.Fatalf("%v: code=%d stderr=%s", args, code, errout.String())
		}
		var result Report
		if e := json.Unmarshal(out.Bytes(), &result); e != nil {
			t.Fatalf("non-JSON response %q: %v", out.String(), e)
		}
	}
	call(0, "scan", r, "--output", "report.json")
	b, e := os.ReadFile(filepath.Join(r, "report.json"))
	if e != nil {
		t.Fatal(e)
	}
	var result Report
	if json.Unmarshal(b, &result) != nil || result.CommandExecution != "NOT_RUN" {
		t.Fatal("invalid stored report")
	}
	call(3, "scan", r, "--output", "report.json")
	for _, name := range []string{"../outside.json", "secrets/report.json", "AGENTS.md", ".ombp/state/journal.json"} {
		call(2, "scan", r, "--output", name)
	}
	call(2, "init", r, "--dry-run", "--output", "new-report.json")
	if _, e = os.Stat(filepath.Join(r, "new-report.json")); !os.IsNotExist(e) {
		t.Fatal("dry-run wrote report")
	}
}

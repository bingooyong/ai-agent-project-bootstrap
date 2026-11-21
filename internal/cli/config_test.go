//go:build darwin || linux

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitConfigBoundToPlanAndCheck(t *testing.T) {
	root := t.TempDir()
	const name = "engineering.yaml"
	const config = "schema_version: '1.0'\nagents: [codex]\nexclude: [broken]\n"
	write := func(relative, body string) {
		t.Helper()
		filename := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(want int, args ...string) []byte {
		t.Helper()
		var out, errOut bytes.Buffer
		if got := Run(context.Background(), append(args, "--format", "json"), &out, &errOut); got != want {
			t.Fatalf("%v: got %d want %d\n%s\n%s", args, got, want, out.String(), errOut.String())
		}
		return out.Bytes()
	}
	write("go.mod", "module example.test/fixture\n\ngo 1.25.0\n")
	write("broken/package.json", "{")
	write(name, config)
	write("same.yaml", config)
	write("invalid-exclude.yaml", "schema_version: '1.0'\nagents: [codex]\nexclude: ['../escape']\n")
	write("null-exclude.yaml", "schema_version: '1.0'\nagents: [codex]\nexclude: null\n")
	if err := os.Symlink(name, filepath.Join(root, "linked.yaml")); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", "missing.yaml", "../engineering.yaml", filepath.Join(root, name), "linked.yaml", ".env", "invalid-exclude.yaml", "null-exclude.yaml"} {
		run(2, "scan", root, "--config", invalid)
		run(2, "check", root, "--config", invalid)
	}
	run(3, "scan", root)
	run(0, "scan", root, "--config", name)
	run(0, "rules", "validate", root, "--config", name)
	planned := filepath.Join(t.TempDir(), "plan.json")
	run(0, "init", root, "--config", name, "--plan-out", planned)
	run(2, "init", root, "--apply", "--plan", planned, "--config", "../engineering.yaml")
	run(4, "init", root, "--apply", "--plan", planned, "--config", "same.yaml")
	write(name, config+"# same effective options, different reviewed bytes\n")
	run(4, "init", root, "--apply", "--plan", planned)
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("failed application produced instructions", err)
	}
	write(name, config)
	run(0, "init", root, "--apply", "--plan", planned)
	run(0, "check", root, "--config", name)
	if result := run(2, "check", root); !bytes.Contains(result, []byte("EXPLICIT_CONFIG_REQUIRED")) {
		t.Fatalf("missing locked config diagnostic: %s", result)
	}
	mutex := filepath.Join(root, ".ombp/state/mutex.json")
	if err := os.MkdirAll(filepath.Dir(mutex), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mutex, []byte(`{"pid":2147483647,"nonce":"00000000000000000000000000000000"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var doctor Report
	if err := json.Unmarshal(run(4, "doctor", root, "--config", name), &doctor); err != nil {
		t.Fatal(err)
	}
	if len(doctor.Findings) != 1 || doctor.Findings[0].Code != "WRITE_MUTEX_PRESENT" || !doctor.Findings[0].Blocking {
		t.Fatalf("doctor missed pending mutex: %#v", doctor.Findings)
	}
	if err := os.Remove(mutex); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Dir(mutex)
	if err := os.RemoveAll(stateDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), stateDir); err != nil {
		t.Fatal(err)
	}
	var incompleteDoctor Report
	if err := json.Unmarshal(run(3, "doctor", root, "--config", name), &incompleteDoctor); err != nil {
		t.Fatal(err)
	}
	if incompleteDoctor.OperationStatus != "incomplete" || incompleteDoctor.Result != "error" {
		t.Fatalf("unsafe transaction state was reported complete: %#v", incompleteDoctor)
	}
	if len(incompleteDoctor.Findings) != 1 || incompleteDoctor.Findings[0].Code != "TRANSACTION_STATE_UNVERIFIED" {
		t.Fatalf("duplicate or incorrect transaction findings: %#v", incompleteDoctor.Findings)
	}
	if err := os.Remove(stateDir); err != nil {
		t.Fatal(err)
	}
	run(2, "doctor", root)
	run(0, "doctor", root, "--config", name)
	if result := run(1, "check", root, "--config", "same.yaml"); !bytes.Contains(result, []byte("CONFIG_DRIFT")) {
		t.Fatalf("missing path drift finding: %s", result)
	}
	run(2, "init", root, "--dry-run")
	if result := run(0, "diff", root, "--config", name); !bytes.Contains(result, []byte(`"changes": []`)) {
		t.Fatalf("replaying selected config is not idempotent: %s", result)
	}
	if _, err := os.Stat(filepath.Join(root, ".ombp/project.yaml")); !os.IsNotExist(err) {
		t.Fatal("explicit config was replaced by default config", err)
	}
}

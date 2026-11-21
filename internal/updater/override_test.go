//go:build darwin || linux

package updater

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bingooyong/oh-my-blueprint/internal/rules"
	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
)

const overrideBase = `---
schema_version: '1.0'
id: project.fixture
title: Fixture rule
version: 1.0.0
category: quality
severity: P2
source: {package: project}
scope: {modules: ['.'], paths: ['**']}
when: {}
overridable: true
dependencies: []
conflicts: []
constraints: []
enforcement: [instruction]
validation: {kind: manual, description: Review instructions}
---
Base fixture review instructions.
`

func TestExplicitOverridePlanBindsSourceAndRecordsLineage(t *testing.T) {
	ctx := context.Background()
	root := fixture(t)
	const directory = ".ombp/rules/custom/"
	const recordFile = ".ombp/rules/overrides/fixture.yaml"
	replacement := strings.NewReplacer("version: 1.0.0", "version: 1.1.0", "Base fixture", "Replacement fixture").Replace(overrideBase)
	parse := func(doc string) rules.Rule {
		t.Helper()
		r, err := rules.Parse([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	before, after := parse(overrideBase), parse(replacement)
	write(t, root, directory+"base.md", overrideBase)
	write(t, root, directory+"replacement.md", replacement)
	if _, err := Build(ctx, root, testVersion); err == nil {
		t.Fatal("differing definitions without an explicit record were accepted")
	}
	record := rules.Override{SchemaVersion: "1.0", RuleID: before.ID, Scope: before.Scope,
		Before: rules.RuleVersion{Version: before.Version, SHA256: before.SHA256},
		After:  rules.RuleVersion{Version: after.Version, SHA256: after.SHA256},
		Owner:  "fixture-reviewer", Reason: "Adopt the new fixture review process"}
	data := string(encode(record))
	write(t, root, recordFile, data)
	p := build(t, root)
	if p.Inputs[recordFile] != safeio.Digest([]byte(data)) {
		t.Fatal("plan is missing the parsed override source digest")
	}
	write(t, root, recordFile, data+"\n")
	if err := Apply(ctx, p, testVersion); err == nil {
		t.Fatal("plan applied after the source record changed")
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("refused plan changed instructions: %v", err)
	}
	write(t, root, recordFile, data)
	if err := Apply(ctx, p, testVersion); err != nil {
		t.Fatal(err)
	}
	if text := read(t, root, "AGENTS.md"); !strings.Contains(text, "Replacement fixture") || strings.Contains(text, "Base fixture") {
		t.Fatal("resolved output did not select only the replacement")
	}
	lockText := read(t, root, lockPath)
	if !strings.Contains(lockText, recordFile) || !strings.Contains(lockText, before.SHA256) || strings.Contains(lockText, record.Reason) {
		t.Fatal("lock is missing lineage or copied unrestricted commentary")
	}
	if next := build(t, root); len(next.Changes) != 0 {
		t.Fatal("override plan is not idempotent")
	}
	for _, name := range []string{recordFile, directory + "replacement.md"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Build(ctx, root, testVersion); err == nil {
		t.Fatal("locked rule version was silently changed by removing the override")
	}
}

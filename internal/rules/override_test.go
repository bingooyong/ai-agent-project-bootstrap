package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bingooyong/oh-my-blueprint/internal/model"
	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
)

func explicitReplacement(t *testing.T, before, after Rule) Override {
	t.Helper()
	before, err := normalize(before)
	if err != nil {
		t.Fatal(err)
	}
	after, err = normalize(after)
	if err != nil {
		t.Fatal(err)
	}
	return Override{
		SchemaVersion: "1.0", RuleID: before.ID,
		Before: RuleVersion{Version: before.Version, SHA256: before.SHA256},
		After:  RuleVersion{Version: after.Version, SHA256: after.SHA256},
		Scope:  before.Scope, Reason: "Use the local convention", Owner: "engineering",
	}
}

func TestExplicitOverrideDeterminism(t *testing.T) {
	for _, severity := range []string{"P2", "P3"} {
		t.Run(severity, func(t *testing.T) {
			before := customRule(t, "project.replacement")
			before.Severity, before.Overridable = severity, true
			after := before
			after.Version, after.Content = "2.0.0", "Use local conventions."
			after.Source.Path = ".ombp/rules/custom/a.md"
			copy := after
			copy.Source.Path = ".ombp/rules/custom/b.md"
			record := explicitReplacement(t, before, after)
			record.Path = overridesDir + "/convention.yaml"
			p := model.Profile{}
			got, err := ResolveWithOverrides(p, []Rule{before, after, copy}, []Override{record})
			if err != nil {
				t.Fatal(err)
			}
			reversed, err := ResolveWithOverrides(p, []Rule{copy, after, before}, []Override{record})
			if err != nil || !reflect.DeepEqual(got, reversed) {
				t.Fatalf("input order changed override resolution: %v", err)
			}
			selected := slices.IndexFunc(got.Rules, func(r Rule) bool { return r.ID == before.ID })
			if len(got.Overrides) != 1 || !reflect.DeepEqual(got.Overrides[0], record) || selected < 0 ||
				got.Rules[selected].Version != after.Version || len(got.Rules[selected].Origins) != 2 {
				t.Fatalf("wrong selection or provenance: %#v", got.Overrides)
			}
			if _, err := Resolve(p, []Rule{before, after}); err == nil {
				t.Fatal("accepted replacement without its record")
			}
			record.Reason = "A new stated reason"
			changed, err := ResolveWithOverrides(p, []Rule{before, after, copy}, []Override{record})
			if err != nil || got.Fingerprint == changed.Fingerprint {
				t.Fatal("record changes did not affect resolution fingerprint", err)
			}
		})
	}
}

func TestOverrideRejectsUnboundOrProtectedReplacement(t *testing.T) {
	for _, tc := range []string{"wrong-hash", "wrong-version", "wrong-scope", "changes-scope", "changes-applicability", "duplicate-record", "ambiguous-third-version", "same-version-different-content", "non-overridable", "missing-target", "missing-replacement", "missing-dependency", "dependency-cycle", "missing-owner", "promotes-protected", "protected-target"} {
		t.Run(tc, func(t *testing.T) {
			before := customRule(t, "project.replacement")
			before.Overridable = true
			after := before
			after.Version, after.Content = "2.0.0", "Use local conventions."
			var companion []Rule
			switch tc {
			case "protected-target":
				before.Severity, before.Overridable = "P1", false
			case "non-overridable":
				before.Overridable = false
			case "promotes-protected":
				after.Severity, after.Overridable = "P0", false
			case "changes-scope":
				after.Scope.Paths = []string{"src/**"}
			case "changes-applicability":
				after.When.Languages = []string{"go"}
			case "missing-dependency":
				after.Dependencies = []Dependency{{ID: "unknown.rule", Version: "1.0.0"}}
			case "dependency-cycle":
				other := customRule(t, "project.other")
				other.Dependencies = []Dependency{{ID: after.ID, Version: after.Version}}
				after.Dependencies = []Dependency{{ID: other.ID, Version: other.Version}}
				companion = []Rule{other}
			}
			record := explicitReplacement(t, before, after)
			extra := append([]Rule{before, after}, companion...)
			switch tc {
			case "wrong-hash":
				record.Before.SHA256 = strings.Repeat("a", 64)
			case "wrong-version":
				record.After.Version = "2.1.0"
			case "wrong-scope":
				record.Scope.Modules = []string{"src"}
			case "missing-owner":
				record.Owner = ""
			case "missing-target":
				extra = extra[1:]
			case "missing-replacement":
				extra = extra[:1]
			case "ambiguous-third-version", "same-version-different-content":
				third := after
				third.Content = "Different bytes."
				if tc == "ambiguous-third-version" {
					third.Version = "3.0.0"
				}
				extra = append(extra, third)
			}
			records := []Override{record}
			if tc == "duplicate-record" {
				records = append(records, record)
			}
			if _, err := ResolveWithOverrides(model.Profile{}, extra, records); err == nil {
				t.Fatal("accepted invalid override")
			}
		})
	}
}

func TestLoadProjectBindsOverrideBytesAndRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	beforeDoc := strings.Replace(customDocument, "overridable: false", "overridable: true", 1)
	afterDoc := strings.Replace(beforeDoc, `version: "1.0.0"`, `version: "2.0.0"`, 1)
	before, err := Parse([]byte(beforeDoc))
	if err != nil {
		t.Fatal(err)
	}
	after, err := Parse([]byte(afterDoc))
	if err != nil {
		t.Fatal(err)
	}
	record := explicitReplacement(t, before, after)
	b, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	name := overridesDir + "/replacement.yaml"
	files := map[string][]byte{
		customDir + "/old.md": []byte(beforeDoc),
		customDir + "/new.md": []byte(afterDoc),
		name:                  b,
	}
	for relative, data := range files {
		full := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	project, err := LoadProject(root)
	if err != nil || len(project.Rules) != 2 || len(project.Overrides) != 1 || project.Overrides[0].Path != name {
		t.Fatalf("load project: %v, rules=%d overrides=%d", err, len(project.Rules), len(project.Overrides))
	}
	for relative, data := range files {
		if project.Inputs[relative] != safeio.Digest(data) {
			t.Fatalf("source digest is not the digest of %s", relative)
		}
	}
	if _, err := ResolveWithOverrides(model.Profile{}, project.Rules, project.Overrides); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, name), filepath.Join(root, overridesDir, "linked.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProject(root); err == nil {
		t.Fatal("override loading accepted a symlink")
	}
}

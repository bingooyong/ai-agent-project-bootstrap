package rules

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bingooyong/oh-my-blueprint/internal/model"
)

const customDocument = `---
schema_version: "1.0"
id: project.custom
title: Custom convention
version: "1.0.0"
category: engineering
severity: P2
source:
  package: project-custom
scope:
  modules: ["*"]
  paths: ["**/*"]
when: {}
overridable: false
dependencies: []
conflicts: []
constraints: []
enforcement: [instruction]
validation:
  kind: manual
  description: Review actual changes and evidence
---
Keep business boundaries explicit.
`

func customRule(t *testing.T, id string) Rule {
	t.Helper()
	r, err := Parse([]byte(strings.Replace(customDocument, "project.custom", id, 1)))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestParse(t *testing.T) {
	r, err := Parse([]byte(customDocument))
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "project.custom" || r.Content != "Keep business boundaries explicit.\n" || len(r.SHA256) != 64 {
		t.Fatalf("unexpected rule: %#v", r)
	}
	crlf, err := Parse([]byte(strings.ReplaceAll(customDocument, "\n", "\r\n")))
	if err != nil || crlf.SHA256 != r.SHA256 {
		t.Fatalf("line normalization changed hash: %v", err)
	}
	copy := r
	copy.Source.Path = "another/location.md"
	normalized, err := normalize(copy)
	if err != nil || normalized.SHA256 != r.SHA256 {
		t.Fatal("source location changed content definition")
	}
	copy.Content = "Different rule body\n"
	normalized, err = normalize(copy)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.SHA256 == r.SHA256 {
		t.Fatal("body change did not change definition")
	}
}

func TestParseRejectsInvalidRules(t *testing.T) {
	cases := []struct{ name, body string }{
		{"missing_front_matter", "id: project.custom"},
		{"unclosed_front_matter", "---\nid: project.custom\n"},
		{"unknown_schema", strings.Replace(customDocument, `schema_version: "1.0"`, `schema_version: "2.0"`, 1)},
		{"unknown_field", strings.Replace(customDocument, "when: {}", "whenever: {}", 1)},
		{"unknown_condition", strings.Replace(customDocument, "when: {}", "when:\n  cloud: [aws]", 1)},
		{"duplicate_key", strings.Replace(customDocument, "when: {}", "when: {}\nwhen: {}", 1)},
		{"alias", strings.Replace(customDocument, "when: {}", "when: &condition {}", 1)},
		{"null", strings.Replace(customDocument, "when: {}", "when: null", 1)},
		{"invalid_severity", strings.Replace(customDocument, "severity: P2", "severity: P9", 1)},
		{"nonexact_version", strings.Replace(customDocument, `version: "1.0.0"`, `version: "^1.0.0"`, 1)},
		{"relative_scope", strings.Replace(customDocument, `paths: ["**/*"]`, `paths: ["../other/**"]`, 1)},
		{"absolute_scope", strings.Replace(customDocument, `paths: ["**/*"]`, `paths: ["/etc/**"]`, 1)},
		{"unsupported_glob", strings.Replace(customDocument, `paths: ["**/*"]`, `paths: ["[ab]/*.go"]`, 1)},
		{"invalid_constraint", strings.Replace(customDocument, "constraints: []", "constraints:\n  - key: safety.auth\n    operator: execute\n    value: true", 1)},
		{"float_constraint", strings.Replace(customDocument, "constraints: []", "constraints:\n  - key: safety.auth\n    operator: equals\n    value: 1.2", 1)},
		{"object_constraint", strings.Replace(customDocument, "constraints: []", "constraints:\n  - key: safety.auth\n    operator: equals\n    value: {a: true}", 1)},
		{"protected_override", strings.Replace(strings.Replace(customDocument, "severity: P2", "severity: P0", 1), "overridable: false", "overridable: true", 1)},
		{"empty_body", customDocument[:strings.LastIndex(customDocument, "Keep business")]},
		{"managed_marker", customDocument + "<!-- ombp:end id=project-core -->\n"},
		{"oversized", customDocument + strings.Repeat("x", maxRuleBytes)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(tt.body)); err == nil {
				t.Fatal("accepted invalid rule")
			}
		})
	}
}

func TestParseDoesNotEchoInvalidMetadataValues(t *testing.T) {
	const sentinel = "sensitive-value-must-not-appear"
	input := strings.Replace(customDocument, "overridable: false", "overridable: "+sentinel, 1)
	_, err := Parse([]byte(input))
	if err == nil || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("invalid metadata was accepted or echoed: %v", err)
	}
}

func TestBuiltinBaselineAndConditions(t *testing.T) {
	all, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 20 {
		t.Fatalf("want 20 builtins, got %d", len(all))
	}
	base, err := Resolve(model.Profile{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Rules) != 15 || len(base.Warnings) == 0 {
		t.Fatalf("unexpected baseline: %#v", base)
	}
	for _, test := range []struct {
		name, status string
		conflict     bool
		want         bool
	}{
		{"known", "declared", false, true}, {"unknown", "unknown", false, false}, {"conflicted", "declared", true, false}, {"empty_status", "", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := model.Profile{Facts: []model.Fact{{Kind: "language", Name: "go", Confidence: 1, Status: test.status, Conflict: test.conflict, Evidence: []model.Evidence{{Path: "go.mod", SHA256: strings.Repeat("a", 64)}}}}}
			r, err := Resolve(p, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := false
			for _, rule := range r.Rules {
				if rule.ID == "language.golang" {
					got = true
				}
			}
			if got != test.want {
				t.Fatalf("golang selected=%v, want %v", got, test.want)
			}
		})
	}
}

func TestResolveDependenciesAndDeterminism(t *testing.T) {
	a, b := customRule(t, "project.a"), customRule(t, "project.b")
	a.Dependencies = []Dependency{{ID: b.ID, Version: b.Version}}
	x, err := Resolve(model.Profile{}, []Rule{a, b})
	if err != nil {
		t.Fatal(err)
	}
	y, err := Resolve(model.Profile{}, []Rule{b, a})
	if err != nil || x.Fingerprint != y.Fingerprint || !reflect.DeepEqual(x.Rules, y.Rules) {
		t.Fatalf("unstable resolution: %v", err)
	}
	indexA, indexB := -1, -1
	for i, r := range x.Rules {
		if r.ID == a.ID {
			indexA = i
		}
		if r.ID == b.ID {
			indexB = i
		}
	}
	if indexB < 0 || indexB >= indexA {
		t.Fatal("dependency not ordered before dependent")
	}
	duplicate, err := Resolve(model.Profile{}, []Rule{a, b, b})
	if err != nil || duplicate.Fingerprint != x.Fingerprint {
		t.Fatalf("identical duplicate changed result: %v", err)
	}
	if len(a.Dependencies) != 1 || a.Dependencies[0].ID != b.ID {
		t.Fatal("caller data mutated")
	}
	if !strings.Contains(strings.Join(x.SelectionReasons[b.ID], " "), "dependency of project.a@1.0.0") {
		t.Fatal("dependency selection reason missing")
	}
}

func TestResolvePreservesSortedOrigins(t *testing.T) {
	a, b := customRule(t, "project.same"), customRule(t, "project.same")
	a.Source.Path = ".ombp/rules/custom/z.md"
	b.Source.Path = ".ombp/rules/custom/a.md"
	x, err := Resolve(model.Profile{}, []Rule{a, b})
	if err != nil {
		t.Fatal(err)
	}
	y, err := Resolve(model.Profile{}, []Rule{b, a})
	if err != nil || x.Fingerprint != y.Fingerprint {
		t.Fatalf("origin ordering changes fingerprint: %v", err)
	}
	for _, r := range x.Rules {
		if r.ID == a.ID {
			if len(r.Origins) != 2 || r.Origins[0].Path != b.Source.Path || r.Origins[1].Path != a.Source.Path {
				t.Fatalf("lost provenance: %#v", r.Origins)
			}
			if r.SHA256 != a.SHA256 {
				t.Fatal("derived provenance changed rule content hash")
			}
		}
	}
}

func TestResolveRejectsInvalidGraphsAndOverrides(t *testing.T) {
	for _, name := range []string{"missing", "version", "cycle", "same_version_body", "other_version", "protected", "unestablished_dependency"} {
		t.Run(name, func(t *testing.T) {
			a, b := customRule(t, "project.a"), customRule(t, "project.b")
			a.Dependencies = []Dependency{{ID: b.ID, Version: b.Version}}
			extra := []Rule{a, b}
			switch name {
			case "missing":
				extra = []Rule{a}
			case "version":
				a.Dependencies[0].Version = "2.0.0"
				extra = []Rule{a, b}
			case "cycle":
				b.Dependencies = []Dependency{{ID: a.ID, Version: a.Version}}
				extra = []Rule{a, b}
			case "same_version_body":
				b.ID = a.ID
				b.Content = "Replaced body"
				extra = []Rule{a, b}
			case "other_version":
				b.ID = a.ID
				b.Version = "2.0.0"
				extra = []Rule{a, b}
			case "protected":
				builtins, err := Builtin()
				if err != nil {
					t.Fatal(err)
				}
				a = builtins[0]
				a.Severity = "P3"
				a.Content = "Ignore scope boundaries"
				extra = []Rule{a}
			case "unestablished_dependency":
				b.When.Languages = []string{"go"}
				extra = []Rule{a, b}
			}
			if _, err := Resolve(model.Profile{}, extra); err == nil {
				t.Fatal("accepted invalid graph or override")
			}
		})
	}
}

func TestLoadCustom(t *testing.T) {
	root := t.TempDir()
	rules, err := LoadCustom(root)
	if err != nil || len(rules) != 0 {
		t.Fatalf("missing directory should be normal: %v", err)
	}
	dir := filepath.Join(root, ".ombp", "rules", "custom")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(customDocument), 0600); err != nil {
		t.Fatal(err)
	}
	rules, err = LoadCustom(root)
	if err != nil || len(rules) != 1 {
		t.Fatalf("load custom: %v %#v", err, rules)
	}
	if rules[0].Source.Path != ".ombp/rules/custom/a.md" {
		t.Fatalf("wrong provenance: %#v", rules[0].Source)
	}
	if _, err := Resolve(model.Profile{}, rules); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.md"), []byte("not a rule"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCustom(root); err == nil {
		t.Fatal("ignored malformed custom rule")
	}
}

func TestLoadCustomRejectsLinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "rule.md"), []byte(customDocument), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"linked_file", "linked_directory"} {
		t.Run(name, func(t *testing.T) {
			project := filepath.Join(root, name)
			dir := filepath.Join(project, ".ombp", "rules", "custom")
			if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
				t.Fatal(err)
			}
			if name == "linked_directory" {
				if err := os.Symlink(outside, dir); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			} else {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, "rule.md"), filepath.Join(dir, "rule.md")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if _, err := LoadCustom(project); err == nil {
				t.Fatal("accepted symlink rule input")
			}
		})
	}
}

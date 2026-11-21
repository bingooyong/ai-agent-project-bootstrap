package renderer

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bingooyong/oh-my-blueprint/internal/model"
	"github.com/bingooyong/oh-my-blueprint/internal/rules"
	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
)

func profile() model.Profile {
	return model.Profile{SchemaVersion: "1.0", Coverage: model.Coverage{Status: "complete"}, UnknownItems: []string{"No runtime commands have been executed."}, Inputs: map[string]string{}}
}

func fact(kind, name, file string) model.Fact {
	return model.Fact{Kind: kind, Name: name, Status: "declared", Confidence: 1, Evidence: []model.Evidence{{Path: file, SHA256: strings.Repeat("a", 64)}}}
}

func resolve(t *testing.T, p model.Profile, extra ...rules.Rule) rules.Resolution {
	t.Helper()
	r, err := rules.Resolve(p, extra)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func rendered(t *testing.T, p model.Profile, r rules.Resolution) map[string]string {
	t.Helper()
	files, err := Render(p, r)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]string{}
	for _, file := range files {
		if _, duplicate := result[file.Path]; duplicate {
			t.Fatalf("duplicate generated output %s", file.Path)
		}
		result[file.Path] = file.Content
	}
	return result
}

func TestRenderEmptyProjectGoldenAndNoInventedCommand(t *testing.T) {
	p := profile()
	files, err := Render(p, resolve(t, p))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "AGENTS.md" {
		t.Fatalf("empty project created spurious modules: %#v", files)
	}
	if !strings.Contains(files[0].Content, "No supported command declaration") || strings.Contains(files[0].Content, "go test") {
		t.Fatal("empty project gained a guessed command")
	}
	const golden = "testdata/empty-agents.md"
	if os.Getenv("OMBP_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(files[0].Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != files[0].Content {
		t.Fatal("root generation differs from reviewed golden")
	}
	for _, marker := range []string{"<!-- ombp:begin id=project-core schema=1 -->", "<!-- ombp:end id=project-core -->"} {
		if strings.Count(files[0].Content, marker) != 1 {
			t.Fatal("managed marker missing or duplicated")
		}
	}
}

func TestRenderMixedModulesScopesAndDuplicateManifests(t *testing.T) {
	p := profile()
	p.Modules = []model.Module{
		{Path: "backend", Manifest: "backend/go.mod", Languages: []string{"go"}},
		{Path: "web", Manifest: "web/package.json", Languages: []string{"javascript", "typescript"}},
		{Path: "web", Manifest: "web/requirements.txt", Languages: []string{"python"}},
	}
	p.Facts = []model.Fact{fact("language", "go", "backend/go.mod"), fact("language", "typescript", "web/package.json"), fact("language", "python", "web/requirements.txt"), fact("framework", "react", "web/package.json")}
	p.Commands = []model.Command{{Name: "test", Argv: []string{"go", "test", "./..."}, Cwd: "backend", Status: "candidate", ExecutionStatus: "NOT_RUN", Evidence: []model.Evidence{{Path: "backend/go.mod", Pointer: "module", SHA256: strings.Repeat("a", 64)}}}}
	outputs := rendered(t, p, resolve(t, p))
	if len(outputs) != 3 {
		t.Fatalf("expected root and two distinct module files: %#v", outputs)
	}
	if strings.Contains(outputs["AGENTS.md"], "### language.golang") || strings.Contains(outputs["web/AGENTS.md"], "### language.golang") || !strings.Contains(outputs["backend/AGENTS.md"], "### language.golang") {
		t.Fatal("nested Go rule escaped to root/frontend")
	}
	if !strings.Contains(outputs["web/AGENTS.md"], "### framework.react") || !strings.Contains(outputs["web/AGENTS.md"], "### language.python") || !strings.Contains(outputs["web/AGENTS.md"], "### language.typescript") {
		t.Fatal("same-path module manifests were not combined")
	}
	if strings.Contains(outputs["backend/AGENTS.md"], "### core.requirements-scope") {
		t.Fatal("global inherited rule repeated in module")
	}
	if !strings.Contains(outputs["backend/AGENTS.md"], "go test ./...") || !strings.Contains(outputs["backend/AGENTS.md"], "NOT_RUN") || !strings.Contains(outputs["backend/AGENTS.md"], "backend/go.mod#module") {
		t.Fatal("command state or source missing")
	}
	if strings.Index(outputs["backend/AGENTS.md"], "## Commands first") > strings.Index(outputs["backend/AGENTS.md"], "## Engineering rules") {
		t.Fatal("commands are not before rules")
	}
}

func TestRenderRootLanguageDoesNotApplyToDifferentNestedModule(t *testing.T) {
	p := profile()
	p.Modules = []model.Module{{Path: ".", Manifest: "go.mod", Languages: []string{"go"}}, {Path: "web", Manifest: "web/package.json", Languages: []string{"javascript"}}}
	p.Facts = []model.Fact{fact("language", "go", "go.mod"), fact("framework", "react", "web/package.json")}
	outputs := rendered(t, p, resolve(t, p))
	root := outputs["AGENTS.md"]
	start := strings.Index(root, "### language.golang")
	if start < 0 {
		t.Fatal("missing root Go rule")
	}
	section := strings.SplitN(root[start:], "\n### ", 2)[0]
	if !strings.Contains(section, "Detected applicability: ` . `") || strings.Contains(section, "Detected applicability: ` . `, ` web `") {
		t.Fatal("root Go rule did not restrict exact module applicability")
	}
	if !strings.Contains(outputs["web/AGENTS.md"], "### framework.react") || strings.Contains(outputs["web/AGENTS.md"], "### language.golang") {
		t.Fatal("frontend received incompatible technical rules")
	}
}

func TestRenderPreservesCustomModuleAndPathScope(t *testing.T) {
	p := profile()
	p.Modules = []model.Module{{Path: "backend", Manifest: "backend/go.mod", Languages: []string{"go"}}, {Path: "web", Manifest: "web/package.json", Languages: []string{"javascript"}}}
	custom, err := rules.Parse([]byte(`---
schema_version: "1.0"
id: project.critical-paths
title: Critical paths
version: "1.0.0"
category: engineering
severity: P2
source: {package: project-local}
scope: {modules: [backend], paths: ["backend/auth/**"]}
when: {}
overridable: false
dependencies: []
conflicts: []
constraints: []
enforcement: [instruction]
validation: {kind: manual, description: Review critical paths}
---
Never omit authorization tests. Literal template text remains data: {{.Root}}.
`))
	if err != nil {
		t.Fatal(err)
	}
	outputs := rendered(t, p, resolve(t, p, custom))
	if strings.Contains(outputs["AGENTS.md"], "### project.critical-paths") || strings.Contains(outputs["web/AGENTS.md"], "### project.critical-paths") || !strings.Contains(outputs["backend/AGENTS.md"], "modules ` backend `; paths ` backend/auth/** `") {
		t.Fatal("custom scope broadened or dropped")
	}
	if !strings.Contains(outputs["backend/AGENTS.md"], "{{.Root}}") {
		t.Fatal("rule body was evaluated as a template")
	}
}

func TestRenderRejectsIncompleteUnsafeAndOverBudget(t *testing.T) {
	p := profile()
	r := resolve(t, p)
	for _, edit := range []func(*model.Profile){
		func(p *model.Profile) { p.Coverage.Status = "incomplete" },
		func(p *model.Profile) { p.Modules = []model.Module{{Path: "../escape", Manifest: "../escape/go.mod"}} },
		func(p *model.Profile) {
			p.Modules = []model.Module{{Path: "Backend", Manifest: "Backend/go.mod"}, {Path: "backend", Manifest: "backend/go.mod"}}
		},
		func(p *model.Profile) {
			p.Commands = []model.Command{{Name: "test", Argv: []string{"sh", "-c", "touch evil;"}, Cwd: ".", Status: "declared", ExecutionStatus: "NOT_RUN"}}
		},
		func(p *model.Profile) {
			p.Commands = []model.Command{{Name: "test", Argv: []string{"go", "test"}, Cwd: ".", Status: "declared", ExecutionStatus: "PASS"}}
		},
	} {
		input := profile()
		edit(&input)
		if _, err := Render(input, r); err == nil {
			t.Fatal("accepted unsafe/incomplete profile")
		}
	}
	r.Rules[0].Content = strings.Repeat("x", 33000)
	if _, err := Render(p, r); err == nil {
		t.Fatal("silently truncated over-budget rules")
	}
}

func TestRenderChecksCumulativeAncestorsNotOnlyEachFile(t *testing.T) {
	p := profile()
	p.Modules = []model.Module{{Path: "nested", Manifest: "nested/go.mod", Languages: []string{"go"}}}
	p.Facts = []model.Fact{fact("language", "go", "nested/go.mod")}
	r := resolve(t, p)
	for i := range r.Rules {
		if r.Rules[i].ID == "language.golang" {
			r.Rules[i].Content = strings.Repeat("x", 16000)
		} else if r.Rules[i].ID == "core.requirements-scope" {
			r.Rules[i].Content = strings.Repeat("y", 16000)
		}
	}
	if _, err := Render(p, r); err == nil {
		t.Fatal("ancestor instruction sum exceeded budget without rejection")
	}
}

func TestRenderDeterministicOrderingAndEscapesMetadata(t *testing.T) {
	p := profile()
	p.Modules = []model.Module{{Path: "api", Manifest: "api/go.mod", Languages: []string{"go"}}, {Path: "web", Manifest: "web/package.json", Languages: []string{"javascript"}}}
	p.Facts = []model.Fact{fact("language", "go", "api/go.mod"), fact("framework", "react", "web/package.json")}
	p.UnknownItems = append(p.UnknownItems, "[click](https://example.invalid)\n# override")
	r := resolve(t, p)
	a, err := Render(p, r)
	if err != nil {
		t.Fatal(err)
	}
	p.Modules[0], p.Modules[1] = p.Modules[1], p.Modules[0]
	p.Facts[0], p.Facts[1] = p.Facts[1], p.Facts[0]
	b, err := Render(p, r)
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("order changed generation: %v", err)
	}
	for _, file := range a {
		if strings.Contains(file.Content, "/Users/") || strings.Contains(file.Content, "[click](") || strings.Contains(file.Content, "\n# override") {
			t.Fatal("unsafe metadata rendered as active Markdown")
		}
	}
	if safeio.Digest([]byte(a[0].Content)) != safeio.Digest([]byte(b[0].Content)) {
		t.Fatal("unstable rendered digest")
	}
}

func TestRenderRejectsSelectedRuleWithNoRepresentableScope(t *testing.T) {
	p := profile()
	r := resolve(t, p)
	r.Rules[0].Scope.Modules = []string{"missing-module"}
	if _, err := Render(p, r); err == nil || !strings.Contains(err.Error(), "cannot be represented") {
		t.Fatalf("unrepresentable selected rule silently dropped: %v", err)
	}
}

func TestRenderCommandsOutsideModuleAreStillVisibleWithTheirCwd(t *testing.T) {
	p := profile()
	p.Commands = []model.Command{{Name: "test", Argv: []string{"make", "test"}, Cwd: "tooling", Status: "candidate", ExecutionStatus: "NOT_RUN", Evidence: []model.Evidence{{Path: "tooling/Makefile", Pointer: "line:1", SHA256: strings.Repeat("a", 64)}}}}
	outputs := rendered(t, p, resolve(t, p))
	if !strings.Contains(outputs["AGENTS.md"], "make test") || !strings.Contains(outputs["AGENTS.md"], "cwd ` tooling `") {
		t.Fatal("command outside an explicit module vanished or got wrong cwd")
	}
}

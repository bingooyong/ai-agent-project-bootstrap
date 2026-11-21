package scanner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bingooyong/oh-my-blueprint/internal/model"
)

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("scanner safe I/O protection is currently implemented only on Darwin and Linux")
	}
	dir := t.TempDir()
	for name, content := range files {
		filename := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func hasFact(p model.Profile, kind, name string) bool {
	for _, fact := range p.Facts {
		if fact.Kind == kind && fact.Name == name {
			return true
		}
	}
	return false
}

func TestScanEmptyAndUnrelatedDirectoriesRemainUnknown(t *testing.T) {
	dir := project(t, map[string]string{"backend/note.txt": "not Go", "frontend/view.txt": "not React"})
	p, err := Scan(t.Context(), dir, DefaultOptions())
	if err != nil || p.Coverage.Status != "complete" {
		t.Fatalf("scan failed: %v %#v", err, p.Coverage)
	}
	if len(p.Modules) != 0 || len(p.Commands) != 0 || len(p.Facts) != 0 || len(p.UnknownItems) != 1 {
		t.Fatalf("guessed project from directory names: %#v", p)
	}
}

func TestScanMixedProjectHasEvidenceAndNeverExecutesScripts(t *testing.T) {
	files := map[string]string{
		"go.mod":                     "module example.com/platform\n\ngo 1.25.0\n\nrequire (\n github.com/gin-gonic/gin v1.10.0\n github.com/jackc/pgx/v5 v5.0.0\n)\n",
		"web/package.json":           `{"name":"web","packageManager":"pnpm@9.0.0","dependencies":{"react":"19","mysql2":"3"},"devDependencies":{"typescript":"5","vitest":"2"},"scripts":{"test":"touch SHOULD_NOT_EXIST; echo TOKEN_MUST_NOT_LEAK","build":"vite build"}}`,
		"web/pnpm-lock.yaml":         "lockfileVersion: 9.0\n",
		"api/pyproject.toml":         "[project]\nname = 'api'\ndependencies = [\n 'fastapi>=0.100',\n 'psycopg[binary]>=3',\n]\n[project.optional-dependencies]\ntest = ['pytest>=8']\n",
		"Makefile":                   "build: \n\t@touch SHOULD_NOT_EXIST\n.PHONY: build\n",
		".github/workflows/ci.yml":   "name: ci\non: [push]\njobs: {}\n",
		"AGENTS.md":                  "DO NOT PARSE THIS AS A PROJECT REQUIREMENT",
		"web/.cursor/rules/test.mdc": "private rule body",
	}
	dir := project(t, files)
	p, err := Scan(t.Context(), dir, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range [][2]string{{"language", "go"}, {"language", "python"}, {"language", "typescript"}, {"language", "javascript"}, {"framework", "react"}, {"framework", "gin"}, {"framework", "fastapi"}, {"database", "mysql"}, {"database", "postgresql"}, {"test_tool", "vitest"}, {"test_tool", "pytest"}, {"ci", "github-actions"}} {
		if !hasFact(p, f[0], f[1]) {
			t.Errorf("missing fact %v", f)
		}
	}
	if len(p.Modules) != 3 {
		t.Fatalf("modules: %#v", p.Modules)
	}
	for _, fact := range p.Facts {
		if len(fact.Evidence) == 0 || len(fact.Evidence[0].SHA256) != 64 || p.Inputs[fact.Evidence[0].Path] != fact.Evidence[0].SHA256 {
			t.Errorf("fact lacks manifest evidence: %#v", fact)
		}
	}
	for _, c := range p.Commands {
		if c.ExecutionStatus != "NOT_RUN" || c.Status == "PASS" {
			t.Errorf("wrong execution status: %#v", c)
		}
		if c.Cwd == "web" && c.Argv[0] != "pnpm" {
			t.Errorf("ignored declared package manager: %#v", c)
		}
	}
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"TOKEN_MUST_NOT_LEAK", "touch SHOULD_NOT_EXIST", "DO NOT PARSE THIS", "private rule body", dir} {
		if strings.Contains(string(body), forbidden) {
			t.Errorf("profile leaked untrusted source %q", forbidden)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "SHOULD_NOT_EXIST")); !os.IsNotExist(err) {
		t.Fatal("project script unexpectedly ran")
	}
	if !reflect.DeepEqual(p.AgentConfigs, []string{"AGENTS.md", "web/.cursor/rules/test.mdc"}) {
		t.Fatalf("missing root/module tool configurations: %#v", p.AgentConfigs)
	}
}

func TestScanRejectsMalformedRelevantDeclarationsWithoutLeakingPayload(t *testing.T) {
	for name, content := range map[string]string{
		"package.json":                      `{"scripts":{"test":"SECRET_REJECTED_PAYLOAD"},"scripts":{}}`,
		"go.mod":                            "module\nrequire (\n",
		"duplicate-go.mod":                  "module example.test/fixture\ngo 1.25\ngo 1.26\n",
		"duplicate-toolchain.mod":           "module example.test/fixture\ngo 1.25\ntoolchain go1.25.1\ntoolchain go1.25.2\n",
		"invalid-go-version.mod":            "module example.test/fixture\ngo 01.25\n",
		"invalid-toolchain.mod":             "module example.test/fixture\ngo 1.25\ntoolchain unsupported\n",
		"invalid-require-version.mod":       "module example.test/fixture\ngo 1.25\nrequire example.test/library latest\n",
		"invalid-require-block-version.mod": "module example.test/fixture\ngo 1.25\nrequire (\nexample.test/library v1.02.3\n)\n",
		"invalid-short-prerelease.mod":      "module example.test/fixture\ngo 1.25\nrequire example.test/library v1.2-rc.1\n",
		"invalid-short-build.mod":           "module example.test/fixture\ngo 1.25\nrequire example.test/library v1+meta\n",
		"nested-go-directive.mod":           "module example.test/fixture\nrequire (\ngo 1.26\n)\n",
		"pyproject.toml":                    "[project]\nname = 'ok'\nname = 'SECRET_REJECTED_PAYLOAD'\n",
		"requirements.txt":                  "bad requirement SECRET_REJECTED_PAYLOAD\n",
	} {
		t.Run(name, func(t *testing.T) {
			filename := name
			if strings.HasSuffix(name, ".mod") {
				filename = "go.mod"
			}
			dir := project(t, map[string]string{filename: content})
			p, err := Scan(t.Context(), dir, DefaultOptions())
			if err == nil || p.Coverage.Status != "incomplete" {
				t.Fatalf("accepted invalid manifest: %v %#v", err, p)
			}
			if strings.Contains(err.Error(), "SECRET_REJECTED_PAYLOAD") {
				t.Fatal("parser error leaked source payload")
			}
		})
	}
}

func TestScanAcceptsGoModuleVersionForms(t *testing.T) {
	for _, version := range []string{
		"v1",
		"v1.2",
		"v1.2.3",
		"v1.2.3-rc.1",
		"v1.2.3-rc.1+build.7",
		"v0.0.0-20260304120000-abcdefabcdef",
		"v2.3.4+incompatible",
	} {
		for _, form := range []string{"inline", "block"} {
			t.Run(version+"/"+form, func(t *testing.T) {
				requirement := "require example.test/library " + version + "\n"
				if form == "block" {
					requirement = "require (\nexample.test/library " + version + "\n)\n"
				}
				dir := project(t, map[string]string{"go.mod": "module example.test/fixture\ngo 1.25\n" + requirement})
				profile, err := Scan(t.Context(), dir, DefaultOptions())
				if err != nil || profile.Coverage.Status != "complete" {
					t.Fatalf("valid module version rejected: %v (%+v)", err, profile.Coverage)
				}
			})
		}
	}
}

func TestScanAcceptsGoVersionAndToolchainForms(t *testing.T) {
	for _, directive := range []string{
		"go 1.26rc1\ntoolchain go1.26rc1-custom\n",
		"go 1.27.0\ntoolchain default\n",
	} {
		t.Run(strings.ReplaceAll(strings.TrimSpace(directive), "\n", "_"), func(t *testing.T) {
			dir := project(t, map[string]string{"go.mod": "module example.test/fixture\n" + directive})
			profile, err := Scan(t.Context(), dir, DefaultOptions())
			if err != nil || profile.Coverage.Status != "complete" {
				t.Fatalf("valid Go version or toolchain was rejected: err=%v coverage=%+v", err, profile.Coverage)
			}
		})
	}
}

func TestScanBudgetsAndCancellationAreNotComplete(t *testing.T) {
	dir := project(t, map[string]string{"go.mod": "module example.com/test\ngo 1.25\n", "nested/child/readme": "ignored"})
	for _, test := range []struct {
		name string
		edit func(*Options)
	}{
		{"file-count", func(o *Options) { o.MaxFiles = 1 }},
		{"file-bytes", func(o *Options) { o.MaxFileBytes = 8 }},
		{"total-bytes", func(o *Options) { o.MaxBytes = 8 }},
		{"depth", func(o *Options) { o.MaxDepth = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			o := DefaultOptions()
			test.edit(&o)
			p, err := Scan(t.Context(), dir, o)
			if err == nil || p.Coverage.Status != "incomplete" {
				t.Fatalf("budget violation claimed complete: %v %#v", err, p.Coverage)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p, err := Scan(ctx, dir, DefaultOptions())
	if err == nil || p.Coverage.Status != "incomplete" {
		t.Fatalf("cancelled scan claimed complete: %v %#v", err, p)
	}
}

func TestScanIgnoresSensitiveAndSymlinkContents(t *testing.T) {
	dir := project(t, map[string]string{
		"go.mod": "module example.com/test\ngo 1.25\n", ".env": "TOP_SECRET",
		"node_modules/dependency/package.json": "INVALID_IF_READ", "private/id_rsa": "TOP_SECRET",
	})
	outside := project(t, map[string]string{"package.json": `{"dependencies":{"react":"secret"}}`})
	if err := os.Symlink(outside, filepath.Join(dir, "external")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, ".env"), filepath.Join(dir, "package.json")); err != nil {
		t.Fatal(err)
	}
	p, err := Scan(t.Context(), dir, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if hasFact(p, "framework", "react") || len(p.Inputs) != 1 {
		t.Fatalf("followed link or read ignored content: %#v", p)
	}
	if p.Coverage.BytesRead != int64(len("module example.com/test\ngo 1.25\n")) {
		t.Fatalf("sensitive or ignored content read: %d", p.Coverage.BytesRead)
	}
}

func TestScanPackageManagerConflictDoesNotGuessCommand(t *testing.T) {
	dir := project(t, map[string]string{
		"package.json": `{"scripts":{"test":"npm test"}}`, "pnpm-lock.yaml": "lockfileVersion: 9", "package-lock.json": `{}`,
	})
	p, err := Scan(t.Context(), dir, DefaultOptions())
	if err != nil || len(p.Commands) != 0 || len(p.UnknownItems) == 0 {
		t.Fatalf("ambiguous manager should have no inferred invocation: %v %#v", err, p)
	}
}

func TestScanTypeScriptAndPythonStaticSubsets(t *testing.T) {
	dir := project(t, map[string]string{
		"compiler/tsconfig.json": `{"compilerOptions":{"strict":true}}`,
		"comments/tsconfig.json": "{ // JSONC\n \"compilerOptions\": {}\n}",
		"requirements.txt":       "# no remote resolution\n-r secret.txt\npytest>=8\nfastapi==0.100\n",
		"api/pyproject.toml":     "[tool.poetry]\nname = 'api'\n[tool.poetry.dependencies]\npython = '^3.12'\npsycopg = '^3'\n[project]\ndynamic = ['dependencies']\n",
	})
	p, err := Scan(t.Context(), dir, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range [][2]string{{"language", "typescript"}, {"language", "python"}, {"database", "postgresql"}, {"test_tool", "pytest"}, {"framework", "fastapi"}} {
		if !hasFact(p, f[0], f[1]) {
			t.Errorf("missing explicit static fact: %v", f)
		}
	}
	if len(p.UnknownItems) < 3 {
		t.Fatalf("unsupported JSONC, indirect requirements and dynamic dependencies must remain unknown: %#v", p.UnknownItems)
	}
	if _, ok := p.Inputs["secret.txt"]; ok {
		t.Fatal("followed requirements include")
	}
	for _, fact := range p.Facts {
		if fact.Evidence[0].Path == "comments/tsconfig.json" {
			t.Fatal("inferred TypeScript from unsupported JSONC filename")
		}
	}
}

func TestScanWorkspacePackageManagerUsesExplicitAncestorEvidence(t *testing.T) {
	for _, test := range []struct {
		name, root, workspace, expected string
		selectors                       []string
	}{
		{"package-workspaces", `{"packageManager":"pnpm@9","workspaces":["other/*","packages/*"]}`, "", "pnpm", []string{"package.json#/packageManager", "package.json#/workspaces/1"}},
		{"workspaces-object", `{"packageManager":"yarn@4","workspaces":{"packages":["packages/*"]}}`, "", "yarn", []string{"package.json#/packageManager", "package.json#/workspaces/packages/0"}},
		{"pnpm-workspace", `{}`, "packages: ['packages/*']\n", "pnpm", []string{"pnpm-workspace.yaml#", "pnpm-workspace.yaml#/packages/0"}},
		{"unrelated-parent", `{"packageManager":"pnpm@9","workspaces":["other/*"]}`, "", "npm", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := map[string]string{"package.json": test.root, "packages/web/package.json": `{"scripts":{"test":"echo do-not-run"}}`}
			if test.workspace != "" {
				files["pnpm-workspace.yaml"] = test.workspace
			}
			p, err := Scan(t.Context(), project(t, files), DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Commands) != 1 || p.Commands[0].Cwd != "packages/web" || p.Commands[0].Argv[0] != test.expected {
				t.Fatalf("workspace invocation mismatch: %#v", p.Commands)
			}
			want := append([]string{"packages/web/package.json#/scripts/test"}, test.selectors...)
			assertNodeCommandEvidence(t, files, p, want)
		})
	}
}

func TestScanLocalNodePackageManagerSelectors(t *testing.T) {
	for _, test := range []struct {
		name      string
		files     map[string]string
		manager   string
		selectors []string
	}{
		{
			name: "package-manager-and-agreeing-locks",
			files: map[string]string{
				"package.json": `{"packageManager":"bun@1","scripts":{"test":"echo do-not-run"}}`,
				"bun.lockb":    "legacy lock bytes", "bun.lock": "text lock bytes",
			},
			manager: "bun", selectors: []string{"package.json#/packageManager", "bun.lock#", "bun.lockb#"},
		},
		{
			name:    "lock-only",
			files:   map[string]string{"package.json": `{"scripts":{"test":"echo do-not-run"}}`, "yarn.lock": "lock bytes"},
			manager: "yarn", selectors: []string{"yarn.lock#"},
		},
		{
			name:    "implicit-npm-convention",
			files:   map[string]string{"package.json": `{"scripts":{"test":"echo do-not-run"}}`},
			manager: "npm",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, err := Scan(t.Context(), project(t, test.files), DefaultOptions())
			if err != nil || len(p.Commands) != 1 || !slices.Equal(p.Commands[0].Argv, []string{test.manager, "run", "test"}) {
				t.Fatalf("invocation mismatch: %v, %#v", err, p.Commands)
			}
			want := append([]string{"package.json#/scripts/test"}, test.selectors...)
			assertNodeCommandEvidence(t, test.files, p, want)
			var managerFacts []model.Fact
			for _, fact := range p.Facts {
				if fact.Kind == "build_tool" {
					managerFacts = append(managerFacts, fact)
				}
			}
			if len(test.selectors) == 0 {
				if len(managerFacts) != 0 {
					t.Fatalf("fallback reported as declared build-tool fact: %#v", managerFacts)
				}
			} else if len(managerFacts) != 1 || managerFacts[0].Name != test.manager || !slices.Equal(managerFacts[0].Evidence, p.Commands[0].Evidence[1:]) {
				t.Fatalf("manager fact and command selectors disagree: %#v, %#v", managerFacts, p.Commands)
			}
		})
	}
}

func assertNodeCommandEvidence(t *testing.T, files map[string]string, p model.Profile, want []string) {
	t.Helper()
	c := p.Commands[0]
	if c.Status != "declared" || c.ExecutionStatus != "NOT_RUN" {
		t.Fatalf("command declaration quality changed: %#v", c)
	}
	matrixEvidence(t, files, p.Inputs, c.Evidence) // All digests must match fixture bytes and the input ledger.
	got := make([]string, 0, len(c.Evidence))
	for _, e := range c.Evidence {
		got = append(got, e.Path+"#"+e.Pointer)
	}
	if !slices.Equal(got, want) {
		t.Errorf("command evidence = %q, want %q", got, want)
	}
}

func TestScanPackageManifestRequiresJSONAndStringScripts(t *testing.T) {
	for _, content := range []string{"name: YAML-is-not-package-JSON", `{"scripts":{"test":42}}`, `{"dependencies":{"react":42}}`, `{"workspaces":42}`} {
		dir := project(t, map[string]string{"package.json": content})
		p, err := Scan(t.Context(), dir, DefaultOptions())
		if err == nil || p.Coverage.Status != "incomplete" {
			t.Fatalf("accepted invalid package JSON %s: %v", content, err)
		}
	}
}

func TestScanDeterminismAndManifestDrift(t *testing.T) {
	files := map[string]string{"package.json": `{"scripts":{"build":"echo value","test":"echo test"},"dependencies":{"react":"19"}}`, "service/go.mod": "module example.com/svc\ngo 1.25\n"}
	a, b := project(t, files), project(t, files)
	pa, err := Scan(t.Context(), a, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	pb, err := Scan(t.Context(), b, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pa, pb) {
		t.Fatal("profile depends on absolute root or traversal order")
	}
	if err := os.WriteFile(filepath.Join(a, "package.json"), []byte(`{"scripts":{"test":"changed"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := Scan(t.Context(), a, DefaultOptions())
	if err != nil || changed.Inputs["package.json"] == pa.Inputs["package.json"] {
		t.Fatalf("manifest drift not tracked: %v", err)
	}
}

func TestScanControlDirectoryDoesNotChangeProjectProfile(t *testing.T) {
	dir := project(t, map[string]string{"go.mod": "module example.com/test\ngo 1.25\n"})
	options := DefaultOptions()
	options.MaxFiles = 1 // The sole source entry exactly consumes the budget.
	before, err := Scan(t.Context(), dir, options)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".ombp", "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ombp", "profile.json"), []byte(`{"private_control_data":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := Scan(t.Context(), dir, options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("product control state changed project fingerprint: before=%#v after=%#v", before, after)
	}
}

func TestScanExclusionsCannotEnableSecrets(t *testing.T) {
	dir := project(t, map[string]string{"packages/ignored/package.json": "MALFORMED", "keep/go.mod": "module example.com/keep\ngo 1.25\n"})
	o := DefaultOptions()
	o.Exclude = []string{"**/ignored"}
	p, err := Scan(t.Context(), dir, o)
	if err != nil || len(p.Modules) != 1 || p.Modules[0].Path != "keep" {
		t.Fatalf("exclude failed: %v %#v", err, p)
	}
	for _, pattern := range []string{"../*", "/tmp", "[", ""} {
		o.Exclude = []string{pattern}
		if _, err := Scan(t.Context(), dir, o); err == nil {
			t.Errorf("accepted unsafe exclusion %q", pattern)
		}
	}
}

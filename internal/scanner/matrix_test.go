package scanner

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/bingooyong/oh-my-blueprint/internal/model"
)

func TestScanMVPFixtureMatrix(t *testing.T) {
	type fixture struct {
		name     string
		files    map[string]string
		facts    []string // kind/name@source#locator
		modules  []string // cwd@manifest
		commands []string // name=cwd:argv|status@source#locator
		unknown  bool
	}
	cases := []fixture{
		{name: "empty", unknown: true},
		{
			name: "unknown-sql-and-directory-names",
			files: map[string]string{
				"backend/db/schema.sql": "CREATE TABLE customer (id NUMBER PRIMARY KEY);\n",
				"frontend/widget.jsx":   "export default function Widget() { return null }\n",
			},
			unknown: true,
		},
		{
			name: "go-multi-module",
			files: map[string]string{
				"go.mod":       "module example.test/platform\ngo 1.25\nrequire github.com/jackc/pgx/v5 v5.7.1\n",
				"tools/go.mod": "module example.test/tools\ngo 1.25\nrequire github.com/go-sql-driver/mysql v1.9.0\n",
			},
			facts: []string{
				"database/mysql@tools/go.mod#line:3",
				"database/postgresql@go.mod#line:3",
				"language/go@go.mod#module",
				"language/go@tools/go.mod#module",
			},
			modules: []string{".@go.mod", "tools@tools/go.mod"},
			commands: []string{
				"build=.:go build ./...|candidate@go.mod#module",
				"build=tools:go build ./...|candidate@tools/go.mod#module",
				"test=.:go test ./...|candidate@go.mod#module",
				"test=tools:go test ./...|candidate@tools/go.mod#module",
			},
		},
		{
			name: "python-explicit-oracle-without-test-command",
			files: map[string]string{
				"api/pyproject.toml": "[project]\nname = 'api'\ndependencies = ['oracledb>=2', 'fastapi>=0.100']\n[tool.pytest.ini_options]\ntestpaths = ['tests']\n",
			},
			facts: []string{
				"database/oracle@api/pyproject.toml#project.dependencies:line:3",
				"framework/fastapi@api/pyproject.toml#project.dependencies:line:3",
				"language/python@api/pyproject.toml#project/build-system",
				"test_tool/pytest@api/pyproject.toml#tool.pytest.ini_options",
			},
			modules: []string{"api@api/pyproject.toml"},
		},
		{
			name: "javascript-only",
			files: map[string]string{
				"package.json": `{"packageManager":"npm@10.9.0","scripts":{"test":"node --test"},"devDependencies":{"jest":"29"}}`,
			},
			facts:    []string{"language/javascript@package.json#/", "test_tool/jest@package.json#/devDependencies/jest"},
			modules:  []string{".@package.json"},
			commands: []string{"test=.:npm run test|declared@package.json#/scripts/test"},
		},
		{
			name: "typescript-react-workspace",
			files: map[string]string{
				"package.json":      `{"packageManager":"pnpm@9.0.0","workspaces":["web"]}`,
				"web/package.json":  `{"dependencies":{"react":"19"},"devDependencies":{"typescript":"5","vitest":"2"},"scripts":{"build":"vite build","test":"vitest run"}}`,
				"web/tsconfig.json": `{"compilerOptions":{"strict":true}}`,
			},
			facts: []string{
				"framework/react@web/package.json#/dependencies/react",
				"language/javascript@package.json#/",
				"language/javascript@web/package.json#/",
				"language/typescript@web/package.json#/devDependencies/typescript",
				"language/typescript@web/tsconfig.json#/compilerOptions",
				"structure/workspace@package.json#/workspaces",
				"test_tool/vitest@web/package.json#/devDependencies/vitest",
			},
			modules: []string{".@package.json", "web@web/package.json"},
			commands: []string{
				"build=web:pnpm run build|declared@web/package.json#/scripts/build",
				"test=web:pnpm run test|declared@web/package.json#/scripts/test",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := Scan(t.Context(), project(t, tc.files), DefaultOptions())
			if err != nil || p.Coverage.Status != "complete" {
				t.Fatalf("incomplete fixture scan: %v (%+v)", err, p.Coverage)
			}
			if (len(p.UnknownItems) > 0) != tc.unknown {
				t.Errorf("unexpected review items: %v", p.UnknownItems)
			}
			var facts, modules, commands []string
			for _, f := range p.Facts {
				source := matrixEvidence(t, tc.files, p.Inputs, f.Evidence)
				switch f.Kind {
				case "language", "framework", "database", "structure", "test_tool":
					if f.Status != "declared" || f.Confidence <= 0 || f.Confidence > 1 {
						t.Errorf("invalid declaration quality: %+v", f)
					}
					facts = append(facts, f.Kind+"/"+f.Name+"@"+source)
				}
			}
			for _, m := range p.Modules {
				modules = append(modules, m.Path+"@"+m.Manifest)
			}
			for _, c := range p.Commands {
				if c.ExecutionStatus != "NOT_RUN" {
					t.Errorf("command claims execution: %+v", c)
				}
				source := matrixEvidence(t, tc.files, p.Inputs, c.Evidence)
				commands = append(commands, c.Name+"="+c.Cwd+":"+strings.Join(c.Argv, " ")+"|"+c.Status+"@"+source)
			}
			for _, group := range []struct {
				name      string
				got, want []string
			}{
				{"facts", facts, tc.facts}, {"modules", modules, tc.modules}, {"commands", commands, tc.commands},
			} {
				slices.Sort(group.got)
				if !slices.Equal(group.got, group.want) {
					t.Errorf("%s = %q, want %q", group.name, group.got, group.want)
				}
			}
		})
	}
}

func matrixEvidence(t *testing.T, files, inputs map[string]string, evidence []model.Evidence) string {
	t.Helper()
	if len(evidence) == 0 {
		t.Fatal("declaration has no source evidence")
	}
	for _, e := range evidence {
		content, ok := files[e.Path]
		if !ok || e.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(content))) || inputs[e.Path] != e.SHA256 {
			t.Errorf("source digest does not match the fixture and input ledger: %+v", e)
		}
	}
	return evidence[0].Path + "#" + evidence[0].Pointer
}

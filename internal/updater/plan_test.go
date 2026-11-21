//go:build darwin || linux

package updater

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
)

const testVersion = "0.1.0-test"

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test/fixture\n\ngo 1.25.0\n")
	return root
}
func write(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
}
func read(t *testing.T, root, name string) string {
	t.Helper()
	b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func build(t *testing.T, root string) Plan {
	t.Helper()
	p, e := Build(context.Background(), root, testVersion)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func initialized(t *testing.T) string {
	t.Helper()
	r := fixture(t)
	if e := Apply(context.Background(), build(t, r), testVersion); e != nil {
		t.Fatal(e)
	}
	return r
}

func TestPlanApplyIsIdempotentAndPreservesManualContent(t *testing.T) {
	r := fixture(t)
	p := build(t, r)
	if _, e := os.Stat(filepath.Join(r, ".ombp")); !os.IsNotExist(e) {
		t.Fatal("preview wrote metadata")
	}
	if len(p.Changes) != 4 {
		t.Fatalf("expected agents/config/profile/lock; got %d", len(p.Changes))
	}
	file := filepath.Join(r, "reviewed-plan.json")
	if e := SavePlan(file, p); e != nil {
		t.Fatal(e)
	}
	decoded, e := ReadPlan(file)
	if e != nil {
		t.Fatal(e)
	}
	if e = Apply(context.Background(), decoded, testVersion); e != nil {
		t.Fatal(e)
	}
	if next := build(t, r); len(next.Changes) != 0 {
		t.Fatalf("non-idempotent paths %#v", next.Changes)
	}
	text := "manual prefix\n" + read(t, r, "AGENTS.md") + "\nmanual suffix\r\n"
	write(t, r, "AGENTS.md", text)
	next := build(t, r)
	if e = Apply(context.Background(), next, testVersion); e != nil {
		t.Fatal(e)
	}
	if got := read(t, r, "AGENTS.md"); got != text {
		t.Fatal("manual text changed")
	}
}

func TestPlanRefusesStaleAndForgedSources(t *testing.T) {
	r := fixture(t)
	p := build(t, r)
	write(t, r, "go.mod", "module example.test/changed\n\ngo 1.25.0\n")
	if e := Apply(context.Background(), p, testVersion); e == nil {
		t.Fatal("accepted stale sources")
	}
	if _, e := os.Stat(filepath.Join(r, "AGENTS.md")); !os.IsNotExist(e) {
		t.Fatal("stale plan mutated outputs")
	}
	p = build(t, r)
	p.Changes[0].Content = "malicious replacement"
	p.Changes[0].AfterSHA256 = safeio.Digest([]byte(p.Changes[0].Content))
	p.Fingerprint = planDigest(p)
	if e := Apply(context.Background(), p, testVersion); e == nil {
		t.Fatal("accepted self-hashed forged plan")
	}
}

func TestBuildProtectsOwnershipAndVersions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, string)
	}{
		{"manual-managed", func(t *testing.T, r string) {
			write(t, r, "AGENTS.md", strings.Replace(read(t, r, "AGENTS.md"), "Engineering instructions", "manual edit", 1))
		}},
		{"missing-lock", func(t *testing.T, r string) {
			if e := os.Remove(filepath.Join(r, lockPath)); e != nil {
				t.Fatal(e)
			}
		}},
		{"override", func(t *testing.T, r string) { write(t, r, "AGENTS.override.md", "existing instructions") }},
		{"damaged-profile", func(t *testing.T, r string) { write(t, r, profilePath, "{}") }},
		{"missing-schema", func(t *testing.T, r string) { write(t, r, DefaultConfigPath, `{"agents":["codex"]}`) }},
		{"manual-budget", func(t *testing.T, r string) {
			write(t, r, "AGENTS.md", strings.Repeat("x", 32768)+read(t, r, "AGENTS.md"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := initialized(t)
			tc.change(t, r)
			before := read(t, r, "AGENTS.md")
			if _, e := Build(context.Background(), r, testVersion); e == nil {
				t.Fatal("unsafe state accepted")
			}
			if read(t, r, "AGENTS.md") != before {
				t.Fatal("preview altered user content")
			}
		})
	}
	r := initialized(t)
	if _, e := Build(context.Background(), r, "new-version"); e == nil {
		t.Fatal("silent tool upgrade accepted")
	}
	r = fixture(t)
	write(t, r, "AGENTS.md", "unmanaged")
	if _, e := Build(context.Background(), r, testVersion); e == nil {
		t.Fatal("unmanaged file adopted")
	}
}

func TestPlanSymlinksAndExitCodes(t *testing.T) {
	r := fixture(t)
	if e := os.Symlink(t.TempDir(), filepath.Join(r, ".ombp")); e != nil {
		t.Fatal(e)
	}
	if _, e := Build(context.Background(), r, testVersion); e == nil {
		t.Fatal("followed config link")
	}
	r = fixture(t)
	write(t, r, "package.json", "{")
	_, e := Build(context.Background(), r, testVersion)
	if ExitCode(e) != 3 {
		t.Fatalf("scan failure code %d", ExitCode(e))
	}
	r = fixture(t)
	write(t, r, DefaultConfigPath, "{}")
	_, e = Build(context.Background(), r, testVersion)
	if ExitCode(e) != 2 {
		t.Fatalf("config error code %d", ExitCode(e))
	}
}

func TestExternalPlanParentPolicy(t *testing.T) {
	r := fixture(t)
	p := build(t, r)
	outside := t.TempDir()
	if e := os.Mkdir(filepath.Join(outside, "secrets"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := SavePlan(filepath.Join(outside, "secrets", "plan.json"), p); e == nil {
		t.Fatal("sensitive ancestor accepted")
	}
	actual := t.TempDir()
	linked := filepath.Join(outside, "linked")
	if e := os.Symlink(actual, linked); e != nil {
		t.Fatal(e)
	}
	if e := SavePlan(filepath.Join(linked, "plan.json"), p); e == nil {
		t.Fatal("external parent link followed")
	}
	if e := SavePlan(filepath.Join(actual, "plan.json"), p); e != nil {
		t.Fatal(e)
	}
	if _, e := ReadPlan(filepath.Join(linked, "plan.json")); e == nil {
		t.Fatal("read followed external parent link")
	}
}

func TestPlanBindsIntermediateInstructions(t *testing.T) {
	r := fixture(t)
	write(t, r, "a/b/go.mod", "module example.test/nested\n\ngo 1.25.0\n")
	write(t, r, "a/AGENTS.md", "manual ancestor\n")
	p := build(t, r)
	write(t, r, "a/AGENTS.md", "changed ancestor\n")
	if e := Apply(context.Background(), p, testVersion); e == nil {
		t.Fatal("intermediate native instructions not bound to plan")
	}
}

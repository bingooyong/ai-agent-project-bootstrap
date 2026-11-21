//go:build darwin || linux

package governance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
)

func TestDocumentReferencesResolveOnlySupportedLinks(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"docs", "service"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "docs/how to.md"), []byte("local guide"), 0600); err != nil {
		t.Fatal(err)
	}
	root := openReferenceTestRoot(t, dir)
	checker := newReferenceChecker(root)
	source := "service/AGENTS.md"
	contents := strings.Join([]string{
		"[Guide](<../docs/how to.md#heading>) and [encoded](../docs/how%20to.md?raw=1)",
		"[Parent directory](../docs/) and [Local heading](#current)",
		"[Remote](https://example.org/missing) and [Relative host](//example.org/missing)",
		"<!-- [Hidden](hidden.md)",
		"[Still hidden](multiline.md) -->",
		"```markdown",
		"[Fenced](fenced.md)",
		"```",
		"`[Inline sample](sample.md)` and ![Image](image.png)",
		"    [Indented](example.md)",
		"[Missing](missing.md)",
	}, "\n")
	findings, incomplete := checker.check(source, []byte(contents))
	if incomplete || len(findings) != 1 {
		t.Fatalf("findings = %#v, incomplete = %v", findings, incomplete)
	}
	finding := findings[0]
	if finding.Code != "DOCUMENT_REFERENCE_MISSING" || finding.Blocking || finding.Severity != "Medium" ||
		finding.Location != (Location{Path: source, Line: 11}) {
		t.Fatalf("unexpected missing reference finding: %#v", finding)
	}
	if again, incomplete := checker.check(source, []byte(contents)); len(again) != 0 || incomplete {
		t.Fatalf("repeated source was rechecked: %#v, incomplete = %v", again, incomplete)
	}
}

func TestDocumentReferencesRefuseUnsafeTargets(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs/guide.md"), []byte("local guide"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"parent-link": "docs", "final-link": "docs/guide.md"} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	root := openReferenceTestRoot(t, dir)
	for _, target := range []string{"parent-link/guide.md", "final-link", "../other.md", "/absolute.md", "file:///outside.md", "C:/windows.md", "%2e%65nv", "secrets/../docs/guide.md"} {
		t.Run(target, func(t *testing.T) {
			findings, incomplete := newReferenceChecker(root).check("AGENTS.md", []byte("[test]("+target+")"))
			if incomplete || len(findings) != 1 || findings[0].Code != "DOCUMENT_REFERENCE_UNSAFE" || !findings[0].Blocking {
				t.Fatalf("findings = %#v, incomplete = %v", findings, incomplete)
			}
			if strings.Contains(findings[0].Evidence, target) {
				t.Fatal("raw link target was exposed in the report")
			}
		})
	}
}

func TestDocumentReferencesBudgetDoesNotReportComplete(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "guide.md"), []byte("local guide"), 0600); err != nil {
		t.Fatal(err)
	}
	root := openReferenceTestRoot(t, dir)
	checker := newReferenceChecker(root)
	findings, incomplete := checker.check("AGENTS.md", []byte(strings.Repeat("[Guide](guide.md)\n", maxReferenceLinks+1)))
	if !incomplete || len(findings) != 1 || findings[0].Code != "DOCUMENT_REFERENCE_BUDGET_EXCEEDED" {
		t.Fatalf("findings = %#v, incomplete = %v", findings, incomplete)
	}
}

func TestDocumentReferencesUnavailableMetadataRemainsUnverified(t *testing.T) {
	root, err := safeio.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	findings, incomplete := newReferenceChecker(root).check("AGENTS.md", []byte("[Guide](guide.md)\n"))
	if !incomplete || len(findings) != 1 || findings[0].Code != "DOCUMENT_REFERENCE_UNVERIFIED" {
		t.Fatalf("findings = %#v, incomplete = %v", findings, incomplete)
	}
}

func openReferenceTestRoot(t *testing.T, dir string) *safeio.Root {
	t.Helper()
	root, err := safeio.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root
}

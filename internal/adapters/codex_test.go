package adapters

import "testing"

func TestCodexCapabilitiesAreExplicit(t *testing.T) {
	c := Codex()
	if c.ID != "codex" || c.Version != "1.0.0" || !c.DirectoryHierarchy || c.NativeImport || c.DefaultMaxLoadedBytes != 32768 || c.ProjectSkillsPath != ".agents/skills" || len(c.UnsupportedFeatures) < 3 {
		t.Fatalf("incomplete Codex contract: %#v", c)
	}
	// Return a fresh capability so callers cannot mutate shared policy state.
	c.RuleFiles[0] = "changed"
	if Codex().RuleFiles[0] != "AGENTS.override.md" {
		t.Fatal("capability shares mutable file list")
	}
}

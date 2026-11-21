// Package adapters describes tool-specific instruction loading capabilities.
package adapters

const CodexVersion = "1.0.0"
const CodexInstructionBudget = 32768

type Capability struct {
	ID                    string   `json:"id"`
	Version               string   `json:"version"`
	RuleFiles             []string `json:"rule_files"`
	DirectoryHierarchy    bool     `json:"directory_hierarchy"`
	NativeImport          bool     `json:"native_import"`
	ProjectSkillsPath     string   `json:"project_skills_path"`
	DefaultMaxLoadedBytes int      `json:"default_max_loaded_bytes"`
	UnsupportedFeatures   []string `json:"unsupported_features"`
}

// Codex describes the supported output contract, not a claim that a particular
// user's Codex process has loaded these instructions or enabled Skills.
func Codex() Capability {
	return Capability{
		ID: "codex", Version: CodexVersion,
		RuleFiles:          []string{"AGENTS.override.md", "AGENTS.md"},
		DirectoryHierarchy: true, NativeImport: false,
		ProjectSkillsPath: ".agents/skills", DefaultMaxLoadedBytes: CodexInstructionBudget,
		UnsupportedFeatures: []string{
			"AGENTS.md does not enforce tool permissions or production approvals",
			"Markdown links are navigation, not native instruction imports",
			"User global instructions and configured byte-budget overrides are not inspected",
			"Existing AGENTS.override.md must be checked before applying generated AGENTS.md",
		},
	}
}

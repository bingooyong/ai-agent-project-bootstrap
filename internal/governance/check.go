// Package governance inspects current facts and locked outputs without mutation.
package governance

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"sort"

	"github.com/bingooyong/oh-my-blueprint/internal/adapters"
	"github.com/bingooyong/oh-my-blueprint/internal/model"
	"github.com/bingooyong/oh-my-blueprint/internal/renderer"
	"github.com/bingooyong/oh-my-blueprint/internal/rules"
	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
	"github.com/bingooyong/oh-my-blueprint/internal/scanner"
	"github.com/bingooyong/oh-my-blueprint/internal/strictio"
	"github.com/bingooyong/oh-my-blueprint/internal/updater"
)

type Location struct {
	Path    string `json:"path"`
	Pointer string `json:"pointer,omitempty"`
	Line    int    `json:"line,omitempty"`
}
type Finding struct {
	Code                 string   `json:"code"`
	Severity             string   `json:"severity"`
	Blocking             bool     `json:"blocking"`
	RuleIDs              []string `json:"rule_ids"`
	Location             Location `json:"location"`
	Evidence             string   `json:"evidence"`
	Impact               string   `json:"impact"`
	Recommendation       string   `json:"recommendation"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
	Method               string   `json:"method"`
}
type Report struct {
	Findings []Finding      `json:"findings"`
	ExitCode int            `json:"exit_code"`
	Coverage model.Coverage `json:"coverage"`
}

func digest(v any) string { b, _ := json.Marshal(v); return safeio.Digest(b) }

// Check verifies metadata and current files. A clean result is not a runtime,
// native Agent-loading or enterprise policy compliance certification.
func Check(ctx context.Context, root, version string) (report Report, err error) {
	return CheckWithConfig(ctx, root, version, "")
}

// CheckWithConfig verifies the project using the selected configuration source.
func CheckWithConfig(ctx context.Context, root, version, selection string) (report Report, err error) {
	report.Findings = []Finding{}
	add := func(code, severity, file, pointer, evidence, impact, fix string, blocking bool) {
		report.Findings = append(report.Findings, Finding{code, severity, blocking, []string{}, Location{Path: file, Pointer: pointer}, evidence, impact, fix, blocking, "static"})
		if report.ExitCode != 3 && blocking {
			report.ExitCode = 4
		} else if report.ExitCode == 0 && severity != "Low" && severity != "Informational" {
			report.ExitCode = 1
		}
	}
	defer func() {
		rank := map[string]int{"Critical": 0, "High": 1, "Medium": 2, "Low": 3, "Informational": 4}
		sort.Slice(report.Findings, func(i, j int) bool {
			a, b := report.Findings[i], report.Findings[j]
			if a.Blocking != b.Blocking {
				return a.Blocking
			}
			if a.Severity != b.Severity {
				return rank[a.Severity] < rank[b.Severity]
			}
			if a.Location.Path != b.Location.Path {
				return a.Location.Path < b.Location.Path
			}
			if a.Location.Line != b.Location.Line {
				return a.Location.Line < b.Location.Line
			}
			if a.Location.Pointer != b.Location.Pointer {
				return a.Location.Pointer < b.Location.Pointer
			}
			if a.Code != b.Code {
				return a.Code < b.Code
			}
			return a.Evidence < b.Evidence
		})
	}()
	r, e := safeio.Open(root)
	if e != nil {
		report.ExitCode = 3
		add("ROOT_UNAVAILABLE", "High", ".", "", "Strict project I/O is unavailable", "Inspection did not finish", "Use a supported platform and accessible project root", false)
		return report, e
	}
	defer r.Close()
	references := newReferenceChecker(r)
	checkReferences := func(name string, contents []byte) {
		findings, incomplete := references.check(name, contents)
		for _, finding := range findings {
			report.Findings = append(report.Findings, finding)
			if finding.Blocking && report.ExitCode != 3 {
				report.ExitCode = 4
			} else if report.ExitCode == 0 {
				report.ExitCode = 1
			}
		}
		if incomplete {
			report.Coverage.Status = "incomplete"
			report.ExitCode = 3
		}
	}
	for _, name := range []string{".ombp/state/journal.json", ".ombp/state/mutex.json"} {
		if _, e := r.Lstat(name); e == nil {
			add("TRANSACTION_STATE_PRESENT", "Critical", name, "", "Pending or active transaction state exists", "Writes require ownership and recovery review", "Inspect the writer or recovery preview; do not delete state blindly", true)
		} else if !errors.Is(e, fs.ErrNotExist) {
			report.ExitCode = 3
			return report, e
		}
	}
	configFile := updater.ConfigLocation(selection)
	c, configBytes, e := updater.LoadConfig(r, selection)
	if e != nil {
		report.ExitCode = 2
		// Report the flag rather than echo an unvalidated or sensitive path.
		if selection != "" {
			configFile = "."
		}
		add("CONFIG_INVALID", "High", configFile, "", "Project configuration is missing, invalid or unsupported", "Configuration cannot be used for inspection", "Check the selected project-relative file and its versioned fields", false)
		report.ExitCode = 2
		return report, nil
	}
	lock, lockBytes, e := updater.LoadLock(r)
	if e != nil {
		add("LOCK_INVALID", "Critical", ".ombp/lock.yaml", "", "Project lock is invalid, linked or unsupported", "Generated ownership cannot be established", "Preserve outputs and review the lock or migration plan", true)
		return report, nil
	}
	if selection == "" && lock.ConfigPath != "" {
		add("EXPLICIT_CONFIG_REQUIRED", "High", ".ombp/lock.yaml", "/config_path", "The lock uses an explicit project configuration", "The project cannot be checked against its selected configuration", "Review the config_path in the lock and pass it with --config", false)
		report.ExitCode = 2
		return report, nil
	}
	if configBytes == nil {
		add("CONFIG_MISSING", "Medium", configFile, "", "No project configuration exists at the selected path", "The locked configuration cannot be confirmed", "Review init or check with the project configuration used to generate the lock", false)
	}
	if lockBytes == nil {
		add("LOCK_MISSING", "Medium", ".ombp/lock.yaml", "", "No project lock exists", "Generated ownership and versions are unverified", "Review init; existing manual rules will be protected", false)
	}
	options := scanner.DefaultOptions()
	options.Exclude = c.Exclude
	p, e := scanner.Scan(ctx, root, options)
	report.Coverage = p.Coverage
	if e != nil {
		report.ExitCode = 3
		add("SCAN_INCOMPLETE", "High", ".", "", "Static scan could not complete within its input and resource policy", "No complete source comparison is available", "Review supported manifests, file permissions and scan budgets", false)
		report.ExitCode = 3
		return report, e
	}
	p = updater.PrepareProfile(p, c, selection)
	for _, unknown := range p.UnknownItems {
		add("PROJECT_FACT_UNKNOWN", "Medium", ".", "", unknown, "The scanner cannot establish this project fact", "Confirm the actual engineering declaration; do not invent commands", false)
	}
	for _, fact := range p.Facts {
		if fact.Conflict {
			add("DECLARATION_CONFLICT", "Medium", configFile, "declarations", "A manual declaration conflicts with detected evidence", "Conditional rules may be inapplicable", "Review the declaration and original manifest evidence", false)
		}
	}
	projectRules, e := rules.LoadProject(root)
	if e != nil {
		add("CUSTOM_RULE_INVALID", "High", ".ombp/rules", "", "Custom rules or override records could not be safely loaded or parsed", "Applicable constraints cannot be established", "Fix the rule or override metadata, scope or file safety issue", true)
		return report, nil
	}
	resolved, e := rules.ResolveWithOverrides(p, projectRules.Rules, projectRules.Overrides)
	if e != nil {
		add("RULE_RESOLUTION_BLOCKED", "High", ".ombp/rules", "", "Deterministic rule resolution found an invalid dependency, definition, override or conflict", "No safe generated rule set is available", "Review rule dependencies, exact versions, scopes and protected constraints", true)
		return report, nil
	}
	if lockBytes == nil {
		if b, e := r.Read("AGENTS.md", 4<<20); e == nil {
			checkReferences("AGENTS.md", b)
		} else if !errors.Is(e, fs.ErrNotExist) {
			add("INSTRUCTIONS_UNREADABLE", "High", "AGENTS.md", "", "Existing instructions cannot be safely inspected", "Reference inspection did not complete", "Review instruction file type, permissions and size", false)
			report.ExitCode = 3
			report.Coverage.Status = "incomplete"
		}
		return report, nil
	}
	if lock.ToolVersion != version || lock.AdapterVersion != renderer.AdapterVersion || lock.TemplateVersion != renderer.TemplateVersion || lock.AdapterFingerprint != digest(adapters.Codex()) || lock.TemplateFingerprint != renderer.TemplateFingerprint() {
		add("GENERATOR_VERSION_DRIFT", "High", ".ombp/lock.yaml", "", "Locked generator metadata differs from the running tool", "Outputs are not reproducible under the current generator", "Use the pinned tool or an explicit reviewed upgrade", true)
	}
	if lock.ConfigFingerprint != digest(c) || updater.ConfigLocation(lock.ConfigPath) != configFile {
		add("CONFIG_DRIFT", "Medium", configFile, "", "Project configuration or its path differs from the locked snapshot", "Rules and outputs may need a planned update", "Check with the locked configuration or review the configuration change and generated diff", false)
	}
	if lock.ResolutionFingerprint != resolved.Fingerprint {
		add("RULESET_DRIFT", "High", ".ombp/lock.yaml", "rules", "Selected rules or their source content differ from the lock", "Existing instructions may be obsolete", "Review rule versions and plan an explicit update", true)
	}
	expectedInventory := []updater.RuleLock{}
	for _, rule := range resolved.Rules {
		expectedInventory = append(expectedInventory, updater.RuleLock{ID: rule.ID, Version: rule.Version, SHA256: rule.SHA256})
	}
	if digest(lock.Rules) != digest(expectedInventory) {
		add("RULE_INVENTORY_MISMATCH", "High", ".ombp/lock.yaml", "rules", "Locked rule identities, versions or hashes do not match resolution", "Required engineering rules may be missing", "Review the actual deterministic rule set and lock provenance", true)
	}
	if digest(lock.Overrides) != digest(updater.LockedOverrides(resolved)) {
		add("RULE_OVERRIDE_INVENTORY_MISMATCH", "High", ".ombp/lock.yaml", "overrides", "Locked override lineage does not match the resolved records", "The source of a replacement rule cannot be confirmed", "Review the current override records and locked rule versions", true)
	}
	if updater.ProfileFingerprint(p, lock.Outputs) != lock.ProfileFingerprint {
		add("PROJECT_SOURCE_DRIFT", "Medium", ".ombp/profile.json", "inputs", "Manifest, command-source or project structure evidence changed", "Stored engineering commands and rules may be stale", "Review scan and diff; static declarations are not execution evidence", false)
	}
	stored, e := r.Read(".ombp/profile.json", 4<<20)
	var old model.Profile
	if e != nil || safeio.Digest(stored) != lock.ProfileSHA256 || strictio.Decode(stored, &old) != nil || old.SchemaVersion != "1.0" {
		add("PROFILE_INVALID", "High", ".ombp/profile.json", "", "Stored profile is missing, modified or incompatible", "Source evidence is not the locked snapshot", "Preserve the file and review recovery or regeneration", true)
	} else {
		commandFindings := commandSourceFindings(old, p)
		report.Findings = append(report.Findings, commandFindings...)
		if len(commandFindings) > 0 && report.ExitCode == 0 {
			report.ExitCode = 1
		}
	}
	if _, ok := lock.Outputs["AGENTS.md"]; !ok {
		add("ROOT_RULES_NOT_LOCKED", "High", "AGENTS.md", "", "The root instruction file is not recorded in the lock", "Root engineering rules are not governed", "Review the lock and original generated output", true)
	}
	outputPaths := make([]string, 0, len(lock.Outputs))
	for name := range lock.Outputs {
		outputPaths = append(outputPaths, name)
	}
	sort.Strings(outputPaths)
	validOutputs := outputPaths[:0]
	for _, name := range outputPaths {
		expected := lock.Outputs[name]
		if safeio.ValidatePath(name) != nil || path.Base(name) != "AGENTS.md" {
			add("LOCK_OUTPUT_UNSAFE", "Critical", ".ombp/lock.yaml", "outputs", "An output path is outside the supported rule-file contract", "Output ownership is invalid", "Do not apply this lock; review its provenance", true)
			continue
		}
		validOutputs = append(validOutputs, name)
		b, e := r.Read(name, 4<<20)
		if e == nil {
			checkReferences(name, b)
		}
		block, blockErr := updater.ManagedBlock(string(b))
		if e != nil || blockErr != nil || safeio.Digest([]byte(block)) != expected.BlockSHA256 {
			add("MANAGED_OUTPUT_CHANGED", "High", name, "project-core", "Generated rule content is missing, modified or unsafe to read", "An update would risk losing manual edits", "Preserve the file and review a three-way migration", true)
		} else if safeio.Digest(b) != expected.SHA256 {
			add("MANUAL_TEXT_CHANGED", "Low", name, "", "Text outside the managed rule block differs", "Manual text may need review and remains protected", "Review and preserve manual content when updating the lock", false)
		}
		override := path.Join(path.Dir(name), "AGENTS.override.md")
		if _, e := r.Lstat(override); e == nil {
			add("OVERRIDE_PRECEDENCE", "High", override, "", "An override file takes precedence over generated instructions", "Codex may load different project rules", "Review the native override instead of assuming AGENTS.md is loaded", true)
		}
	}
	for _, name := range validOutputs {
		total := 0
		for dir := path.Dir(name); ; dir = path.Dir(dir) {
			ancestor := path.Join(dir, "AGENTS.md")
			b, e := r.Read(ancestor, 4<<20)
			if e == nil {
				total += len(b)
				checkReferences(ancestor, b)
			} else if !errors.Is(e, fs.ErrNotExist) {
				add("ANCESTOR_UNREADABLE", "High", ancestor, "", "Ancestor instructions cannot be safely inspected", "Native instruction coverage is unknown", "Review ancestor file type, permissions and size", true)
			}
			override := path.Join(dir, "AGENTS.override.md")
			if _, e := r.Lstat(override); e == nil {
				add("ANCESTOR_OVERRIDE_PRECEDENCE", "High", override, "", "An ancestor override changes native instruction loading", "Generated instructions are not the effective rule chain", "Review the actual native override chain", true)
			}
			if dir == "." {
				break
			}
		}
		if total > adapters.CodexInstructionBudget {
			add("INSTRUCTION_BUDGET_EXCEEDED", "High", name, "", "Manual and managed ancestor instructions exceed the default Codex byte budget", "Required rules may be omitted by the native tool", "Review complete ancestor files; do not silently truncate rules", true)
		}
	}
	if rendered, e := renderer.Render(p, resolved); e != nil {
		add("RENDER_BLOCKED", "High", "AGENTS.md", "", "Current rule scope or instruction budget cannot be represented", "No complete instruction output can be generated", "Review module scope and cumulative instruction size", true)
	} else {
		expectedPaths := map[string]bool{}
		unchanged := updater.ProfileFingerprint(p, lock.Outputs) == lock.ProfileFingerprint && resolved.Fingerprint == lock.ResolutionFingerprint
		for _, file := range rendered {
			expectedPaths[file.Path] = true
			if _, exists := lock.Outputs[file.Path]; !exists {
				add("REQUIRED_OUTPUT_NOT_LOCKED", "High", file.Path, "", "A required instruction scope is missing from the lock", "Applicable rules have no governed output", "Review the module rule output set", true)
				continue
			}
			if unchanged {
				expected, _ := updater.ManagedBlock(file.Content)
				actual, e := r.Read(file.Path, 4<<20)
				block, be := updater.ManagedBlock(string(actual))
				if e != nil || be != nil || block != expected {
					add("REQUIRED_RULE_CONTENT_MISMATCH", "High", file.Path, "project-core", "Managed content differs from deterministic compilation despite matching source fingerprints", "Required rules may have been removed even if editable hashes agree", "Reconcile actual rule content and lock using reviewed sources", true)
				}
			}
		}
		for name := range lock.Outputs {
			if !expectedPaths[name] {
				add("OBSOLETE_OUTPUT_SCOPE", "Medium", name, "", "Locked module scope is no longer in the current project", "Rules may describe a removed module", "Plan an explicit scope migration; do not delete manual files", false)
			}
		}
	}
	return report, nil
}

// Package updater constructs verified local plans and applies recoverable writes.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/bingooyong/oh-my-blueprint/internal/adapters"
	"github.com/bingooyong/oh-my-blueprint/internal/model"
	"github.com/bingooyong/oh-my-blueprint/internal/renderer"
	"github.com/bingooyong/oh-my-blueprint/internal/rules"
	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
	"github.com/bingooyong/oh-my-blueprint/internal/scanner"
	"github.com/bingooyong/oh-my-blueprint/internal/strictio"
)

const begin = "<!-- ombp:begin id=project-core schema=1 -->"
const end = "<!-- ombp:end id=project-core -->"
const DefaultConfigPath = ".ombp/project.yaml"
const profilePath = ".ombp/profile.json"
const lockPath = ".ombp/lock.yaml"

var ErrExplicitConfigRequired = errors.New("the lock uses an explicit configuration; select it with --config")

type Declaration struct {
	Kind   string `json:"kind" yaml:"kind"`
	Name   string `json:"name" yaml:"name"`
	Value  string `json:"value" yaml:"value"`
	Reason string `json:"reason" yaml:"reason"`
}

type Config struct {
	SchemaVersion string        `json:"schema_version" yaml:"schema_version"`
	Agents        []string      `json:"agents" yaml:"agents"`
	Exclude       []string      `json:"exclude" yaml:"exclude" schema:"optional"`
	Declarations  []Declaration `json:"declarations" yaml:"declarations" schema:"optional"`
}

type Output struct {
	SHA256      string `json:"sha256" yaml:"sha256"`
	BlockSHA256 string `json:"block_sha256" yaml:"block_sha256"`
}
type RuleLock struct {
	ID      string `json:"id" yaml:"id"`
	Version string `json:"version" yaml:"version"`
	SHA256  string `json:"sha256" yaml:"sha256"`
}
type OverrideLock struct {
	RuleID string            `json:"rule_id" yaml:"rule_id"`
	Before rules.RuleVersion `json:"before" yaml:"before"`
	After  rules.RuleVersion `json:"after" yaml:"after"`
	Path   string            `json:"path" yaml:"path"`
}

// LockedOverrides records replacement lineage without copying free-form notes.
func LockedOverrides(resolved rules.Resolution) []OverrideLock {
	var locked []OverrideLock
	for _, record := range resolved.Overrides {
		locked = append(locked, OverrideLock{record.RuleID, record.Before, record.After, record.Path})
	}
	return locked
}

type Lock struct {
	SchemaVersion         string            `json:"schema_version" yaml:"schema_version"`
	ToolVersion           string            `json:"tool_version" yaml:"tool_version"`
	AdapterVersion        string            `json:"adapter_version" yaml:"adapter_version"`
	TemplateVersion       string            `json:"template_version" yaml:"template_version"`
	ConfigFingerprint     string            `json:"config_fingerprint" yaml:"config_fingerprint"`
	ConfigPath            string            `json:"config_path,omitempty" yaml:"config_path,omitempty"`
	AdapterFingerprint    string            `json:"adapter_fingerprint" yaml:"adapter_fingerprint"`
	TemplateFingerprint   string            `json:"template_fingerprint" yaml:"template_fingerprint"`
	ProfileFingerprint    string            `json:"profile_fingerprint" yaml:"profile_fingerprint"`
	ProfileSHA256         string            `json:"profile_sha256" yaml:"profile_sha256"`
	ResolutionFingerprint string            `json:"resolution_fingerprint" yaml:"resolution_fingerprint"`
	Rules                 []RuleLock        `json:"rules" yaml:"rules"`
	Outputs               map[string]Output `json:"outputs" yaml:"outputs"`
	Overrides             []OverrideLock    `json:"overrides,omitempty" yaml:"overrides,omitempty"`
}
type Change struct {
	Path         string `json:"path" yaml:"path"`
	BeforeSHA256 string `json:"before_sha256" yaml:"before_sha256"`
	AfterSHA256  string `json:"after_sha256" yaml:"after_sha256"`
	Content      string `json:"content" yaml:"content"`
	Diff         string `json:"diff" yaml:"diff"`
}
type Plan struct {
	SchemaVersion string            `json:"schema_version" yaml:"schema_version"`
	ToolVersion   string            `json:"tool_version" yaml:"tool_version"`
	Root          string            `json:"root" yaml:"root"`
	Inputs        map[string]string `json:"inputs" yaml:"inputs"`
	Changes       []Change          `json:"changes" yaml:"changes"`
	Fingerprint   string            `json:"fingerprint" yaml:"fingerprint"`
	ConfigPath    string            `json:"config_path,omitempty" yaml:"config_path,omitempty"`
}

func encode(v any) []byte    { b, _ := json.MarshalIndent(v, "", "  "); return append(b, '\n') }
func canonical(v any) string { b, _ := json.Marshal(v); return safeio.Digest(b) }

func readOptional(r *safeio.Root, name string) ([]byte, error) {
	b, err := r.Read(name, 4<<20)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// ConfigLocation gives the project-relative source for a configuration selection.
func ConfigLocation(selection string) string {
	if selection == "" {
		return DefaultConfigPath
	}
	return selection
}

// LoadConfig uses defaults only when the implicit configuration is absent.
func LoadConfig(r *safeio.Root, selection string) (Config, []byte, error) {
	name := ConfigLocation(selection)
	if err := ValidateConfigPath(selection); err != nil {
		return Config{}, nil, err
	}
	b, err := readOptional(r, name)
	if err != nil {
		return Config{}, nil, err
	}
	if b == nil && selection != "" {
		return Config{}, nil, errors.New("explicit project configuration does not exist")
	}
	c := Config{"1.0", []string{"codex"}, []string{}, []Declaration{}}
	if b != nil {
		c = Config{}
		if err := strictio.Decode(b, &c); err != nil {
			return c, b, errors.New("invalid OMBP project configuration")
		}
	}
	if c.Exclude == nil {
		c.Exclude = []string{}
	}
	if c.Declarations == nil {
		c.Declarations = []Declaration{}
	}
	if c.SchemaVersion != "1.0" || len(c.Agents) != 1 || c.Agents[0] != "codex" {
		return c, b, errors.New("unsupported project schema or Agent selection")
	}
	if err := scanner.ValidateExclusions(c.Exclude); err != nil {
		return c, b, err
	}
	if len(c.Declarations) > 256 {
		return c, b, errors.New("declaration count exceeds budget")
	}
	for _, d := range c.Declarations {
		if d.Kind == "" || d.Name == "" || d.Value == "" || d.Reason == "" || len(d.Value) > 256 {
			return c, b, errors.New("manual declarations require bounded values and reasons")
		}
	}
	return c, b, nil
}

// ProfileFingerprint omits diagnostic counters and tool-owned output discovery.
// The saved profile retains its actual coverage; only its semantic digest omits
// counters that naturally change after generation.
func ProfileFingerprint(p model.Profile, outputs map[string]Output) string {
	p.Coverage.FilesVisited = 0
	p.Coverage.BytesRead = 0
	p.Coverage.CountersOmitted = true
	p.AgentConfigs = append([]string{}, p.AgentConfigs...)
	filtered := p.AgentConfigs[:0]
	for _, name := range p.AgentConfigs {
		if _, owned := outputs[name]; !owned {
			filtered = append(filtered, name)
		}
	}
	p.AgentConfigs = filtered
	return canonical(p)
}

func declarations(p *model.Profile, c Config, source string) {
	for _, d := range c.Declarations {
		found := false
		for i := range p.Facts {
			f := &p.Facts[i]
			if f.Kind == d.Kind && f.Name == d.Name {
				f.DeclaredValue = d.Value
				f.DeclaredSource = source
				f.Conflict = f.Value != "" && f.Value != d.Value
				found = true
			}
		}
		if !found {
			p.Facts = append(p.Facts, model.Fact{Kind: d.Kind, Name: d.Name, DeclaredValue: d.Value, DeclaredSource: source, Status: "manual-unverified", Evidence: []model.Evidence{}})
			p.UnknownItems = append(p.UnknownItems, "manual declaration without detected evidence: "+d.Kind+"/"+d.Name)
		}
	}
	sort.Slice(p.Facts, func(i, j int) bool { return p.Facts[i].Kind+"/"+p.Facts[i].Name < p.Facts[j].Kind+"/"+p.Facts[j].Name })
}

// PrepareProfile retains detected facts when annotating manual declarations.
func PrepareProfile(p model.Profile, c Config, selection string) model.Profile {
	p.Facts = append([]model.Fact{}, p.Facts...)
	p.UnknownItems = append([]string{}, p.UnknownItems...)
	declarations(&p, c, ConfigLocation(selection))
	return p
}

// ManagedBlock validates the ownership delimiters; it does not establish that
// a caller owns the content, which also requires a matching lock digest.
func ManagedBlock(text string) (string, error) { _, _, b, e := managed(text); return b, e }

func managed(text string) (int, int, string, error) {
	if !utf8.ValidString(text) || strings.Count(text, begin) != 1 || strings.Count(text, end) != 1 {
		return 0, 0, "", errors.New("managed block is missing, duplicated or invalid")
	}
	a, b := strings.Index(text, begin), strings.Index(text, end)+len(end)
	if b < a {
		return 0, 0, "", errors.New("managed markers are out of order")
	}
	return a, b, text[a:b], nil
}

func LoadLock(r *safeio.Root) (Lock, []byte, error) {
	b, err := readOptional(r, lockPath)
	if err != nil {
		return Lock{}, nil, err
	}
	var lock Lock
	if b != nil {
		if strictio.Decode(b, &lock) != nil || lock.SchemaVersion != "1.0" || len(lock.Outputs) == 0 || ValidateConfigPath(lock.ConfigPath) != nil {
			return lock, b, errors.New("invalid or unsupported OMBP lock")
		}
	}
	return lock, b, nil
}

// Build never writes files and refuses to adopt any unmanaged existing output.
func Build(ctx context.Context, root, version string) (Plan, error) {
	return BuildWithConfig(ctx, root, version, "")
}

// BuildWithConfig binds an explicit configuration source into the reviewed plan.
func BuildWithConfig(ctx context.Context, root, version, selection string) (Plan, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Plan{}, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return Plan{}, err
	}
	r, err := safeio.Open(abs)
	if err != nil {
		return Plan{}, classified(3, err)
	}
	defer r.Close()
	if _, err := r.Lstat(".ombp/state/journal.json"); err == nil {
		return Plan{}, errors.New("unfinished transaction: recover before planning")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Plan{}, err
	}
	c, configBytes, err := LoadConfig(r, selection)
	if err != nil {
		return Plan{}, classified(2, err)
	}
	old, lockBytes, err := LoadLock(r)
	if err != nil {
		return Plan{}, err
	}
	if lockBytes != nil && old.ConfigPath != "" && selection == "" {
		return Plan{}, classified(2, ErrExplicitConfigRequired)
	}
	if lockBytes != nil && old.ToolVersion != version {
		return Plan{}, errors.New("tool version differs from locked version; explicit upgrade is required")
	}
	options := scanner.DefaultOptions()
	options.Exclude = c.Exclude
	p, err := scanner.Scan(ctx, abs, options)
	if err != nil {
		return Plan{}, classified(3, err)
	}
	p = PrepareProfile(p, c, selection)
	projectRules, err := rules.LoadProject(abs)
	if err != nil {
		return Plan{}, err
	}
	resolved, err := rules.ResolveWithOverrides(p, projectRules.Rules, projectRules.Overrides)
	if err != nil {
		return Plan{}, err
	}
	if lockBytes != nil {
		if old.AdapterVersion != renderer.AdapterVersion || old.TemplateVersion != renderer.TemplateVersion || old.AdapterFingerprint != canonical(adapters.Codex()) || old.TemplateFingerprint != renderer.TemplateFingerprint() {
			return Plan{}, errors.New("locked adapter or template changed; explicit upgrade is required")
		}
		for _, before := range old.Rules {
			for _, now := range resolved.Rules {
				if before.ID == now.ID && (before.Version != now.Version || before.SHA256 != now.SHA256) {
					return Plan{}, errors.New("locked rule content or version changed; explicit upgrade is required")
				}
			}
		}
	}
	files, err := renderer.Render(p, resolved)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{SchemaVersion: "1.0", ToolVersion: version, Root: abs, Inputs: map[string]string{}, Changes: []Change{}, ConfigPath: selection}
	for name, hash := range p.Inputs {
		plan.Inputs[name] = hash
	}
	plan.Inputs[ConfigLocation(selection)] = safeio.Digest(configBytes)
	plan.Inputs[lockPath] = safeio.Digest(lockBytes)
	for name, hash := range projectRules.Inputs {
		plan.Inputs[name] = hash
	}
	outputs := map[string]Output{}
	sourceMatchesLock := lockBytes != nil && old.ProfileFingerprint == ProfileFingerprint(p, old.Outputs) && old.ResolutionFingerprint == resolved.Fingerprint
	if sourceMatchesLock {
		expected := []RuleLock{}
		for _, rule := range resolved.Rules {
			expected = append(expected, RuleLock{rule.ID, rule.Version, rule.SHA256})
		}
		if canonical(expected) != canonical(old.Rules) || canonical(LockedOverrides(resolved)) != canonical(old.Overrides) {
			return Plan{}, errors.New("locked rule inventory does not match deterministic resolution")
		}
	}
	for _, f := range files {
		if err := safeio.ValidatePath(f.Path); err != nil || path.Base(f.Path) != "AGENTS.md" {
			return Plan{}, errors.New("renderer produced an unsupported output path")
		}
		override := path.Join(path.Dir(f.Path), "AGENTS.override.md")
		if _, e := r.Lstat(override); e == nil {
			return Plan{}, errors.New("existing AGENTS.override.md takes precedence; no files changed")
		} else if !errors.Is(e, fs.ErrNotExist) {
			return Plan{}, e
		}
		b, e := readOptional(r, f.Path)
		if e != nil {
			return Plan{}, e
		}
		_, _, newBlock, e := managed(f.Content)
		if e != nil {
			return Plan{}, e
		}
		content := f.Content
		if b != nil {
			a, z, block, e := managed(string(b))
			ref, owned := old.Outputs[f.Path]
			if e != nil || !owned || safeio.Digest([]byte(block)) != ref.BlockSHA256 {
				return Plan{}, fmt.Errorf("output %s is unmanaged or manually changed", f.Path)
			}
			if sourceMatchesLock && block != newBlock {
				return Plan{}, errors.New("managed content differs from deterministic rule compilation")
			}
			content = string(b[:a]) + newBlock + string(b[z:])
		}
		outputs[f.Path] = Output{safeio.Digest([]byte(content)), safeio.Digest([]byte(newBlock))}
		plan.add(f.Path, b, []byte(content))
	}
	for name := range outputs {
		total := 0
		for parent := path.Dir(name); ; parent = path.Dir(parent) {
			file := path.Join(parent, "AGENTS.md")
			override := path.Join(parent, "AGENTS.override.md")
			if _, e := r.Lstat(override); e == nil {
				return Plan{}, errors.New("ancestor override takes precedence over generated instructions")
			} else if !errors.Is(e, fs.ErrNotExist) {
				return Plan{}, e
			}
			plan.Inputs[override] = ""
			var found bool
			for _, change := range plan.Changes {
				if change.Path == file {
					total += len(change.Content)
					found = true
					break
				}
			}
			if !found {
				b, e := readOptional(r, file)
				if e != nil {
					return Plan{}, e
				}
				total += len(b)
				if b != nil {
					plan.Inputs[file] = safeio.Digest(b)
				} else {
					plan.Inputs[file] = ""
				}
			}
			if parent == "." {
				break
			}
		}
		if total > adapters.CodexInstructionBudget {
			return Plan{}, errors.New("manual and managed instructions exceed cumulative byte budget")
		}
	}
	for name := range old.Outputs {
		if _, ok := outputs[name]; !ok {
			return Plan{}, errors.New("previously managed module is no longer present; explicit migration is required")
		}
	}
	fingerprint := ProfileFingerprint(p, outputs)
	profileBytes, e := readOptional(r, profilePath)
	if e != nil {
		return Plan{}, e
	}
	if profileBytes != nil && (lockBytes == nil || safeio.Digest(profileBytes) != old.ProfileSHA256) {
		return Plan{}, errors.New("stored profile is unmanaged or manually changed")
	}
	// Persist stable semantic evidence, not incidental enumeration counts. Exact
	// counters remain available from scan reports. A plan saved inside the root
	// is not a source declaration and must not make its own plan stale.
	p.Coverage.FilesVisited = 0
	p.Coverage.BytesRead = 0
	p.Coverage.CountersOmitted = true
	newProfile := encode(p)
	if old.ProfileFingerprint == fingerprint && profileBytes != nil {
		newProfile = profileBytes
	}
	plan.add(profilePath, profileBytes, newProfile)
	if configBytes == nil {
		plan.add(DefaultConfigPath, nil, encode(c))
	}
	lock := Lock{SchemaVersion: "1.0", ToolVersion: version, AdapterVersion: renderer.AdapterVersion, TemplateVersion: renderer.TemplateVersion, ConfigFingerprint: canonical(c), AdapterFingerprint: canonical(adapters.Codex()), TemplateFingerprint: renderer.TemplateFingerprint(), ProfileFingerprint: fingerprint, ProfileSHA256: safeio.Digest(newProfile), ResolutionFingerprint: resolved.Fingerprint, Rules: []RuleLock{}, Outputs: outputs}
	lock.Overrides = LockedOverrides(resolved)
	if selection != DefaultConfigPath {
		lock.ConfigPath = selection
	}
	for _, rule := range resolved.Rules {
		lock.Rules = append(lock.Rules, RuleLock{rule.ID, rule.Version, rule.SHA256})
	}
	plan.add(lockPath, lockBytes, encode(lock))
	sort.SliceStable(plan.Changes, func(i, j int) bool {
		if plan.Changes[i].Path == lockPath {
			return false
		}
		if plan.Changes[j].Path == lockPath {
			return true
		}
		return plan.Changes[i].Path < plan.Changes[j].Path
	})
	plan.Fingerprint = planDigest(plan)
	return plan, nil
}

func (p *Plan) add(name string, old, new []byte) {
	before := ""
	if old != nil {
		before = safeio.Digest(old)
	}
	p.Inputs[name] = before
	if string(old) == string(new) {
		return
	}
	oldLines, newLines := len(strings.Split(strings.TrimSuffix(string(old), "\n"), "\n")), len(strings.Split(strings.TrimSuffix(string(new), "\n"), "\n"))
	oldStart, newStart := 1, 1
	if len(old) == 0 {
		oldLines, oldStart = 0, 0
	}
	if len(new) == 0 {
		newLines, newStart = 0, 0
	}
	diff := fmt.Sprintf("--- a/%s\n+++ b/%s\n@@ -%d,%d +%d,%d @@\n", name, name, oldStart, oldLines, newStart, newLines)
	for _, line := range strings.Split(strings.TrimSuffix(string(old), "\n"), "\n") {
		if old != nil {
			diff += "-" + line + "\n"
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(new), "\n"), "\n") {
		diff += "+" + line + "\n"
	}
	p.Changes = append(p.Changes, Change{name, before, safeio.Digest(new), string(new), diff})
}
func planDigest(p Plan) string {
	p.Fingerprint = ""
	p.Changes = append([]Change{}, p.Changes...)
	for i := range p.Changes {
		p.Changes[i].Diff = ""
	}
	return canonical(p)
}

func ParsePlan(b []byte) (Plan, error) {
	var p Plan
	if strictio.Decode(b, &p) != nil || p.SchemaVersion != "1.0" || p.Fingerprint == "" || len(p.Changes) > 128 || ValidateConfigPath(p.ConfigPath) != nil {
		return p, errors.New("invalid plan schema")
	}
	if planDigest(p) != p.Fingerprint {
		return p, errors.New("plan fingerprint mismatch")
	}
	return p, nil
}

// ValidateConfigPath rejects unsafe or nonportable explicit config paths.
// An empty selection uses the implicit configuration.
func ValidateConfigPath(name string) error {
	if name != "" && (name == "." || safeio.ValidatePath(name) != nil || safeio.Sensitive(name)) {
		return errors.New("configuration path must be a safe project-relative file")
	}
	return nil
}

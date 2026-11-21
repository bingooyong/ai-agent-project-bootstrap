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

	"github.com/bingooyong/oh-my-blueprint/internal/model"
	"github.com/bingooyong/oh-my-blueprint/internal/renderer"
	"github.com/bingooyong/oh-my-blueprint/internal/rules"
	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
	"github.com/bingooyong/oh-my-blueprint/internal/scanner"
	"github.com/bingooyong/oh-my-blueprint/internal/strictio"
)

const begin = "<!-- ombp:begin id=project-core schema=1 -->"
const end = "<!-- ombp:end id=project-core -->"
const configPath = ".ombp/project.yaml"
const profilePath = ".ombp/profile.json"
const lockPath = ".ombp/lock.yaml"

type Declaration struct {
	Kind   string `json:"kind" yaml:"kind"`
	Name   string `json:"name" yaml:"name"`
	Value  string `json:"value" yaml:"value"`
	Reason string `json:"reason" yaml:"reason"`
}

type Config struct {
	SchemaVersion string        `json:"schema_version" yaml:"schema_version"`
	Agents        []string      `json:"agents" yaml:"agents"`
	Exclude       []string      `json:"exclude" yaml:"exclude"`
	Declarations  []Declaration `json:"declarations" yaml:"declarations"`
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
type Lock struct {
	SchemaVersion         string            `json:"schema_version" yaml:"schema_version"`
	ToolVersion           string            `json:"tool_version" yaml:"tool_version"`
	AdapterVersion        string            `json:"adapter_version" yaml:"adapter_version"`
	TemplateVersion       string            `json:"template_version" yaml:"template_version"`
	ProfileFingerprint    string            `json:"profile_fingerprint" yaml:"profile_fingerprint"`
	ProfileSHA256         string            `json:"profile_sha256" yaml:"profile_sha256"`
	ResolutionFingerprint string            `json:"resolution_fingerprint" yaml:"resolution_fingerprint"`
	Rules                 []RuleLock        `json:"rules" yaml:"rules"`
	Outputs               map[string]Output `json:"outputs" yaml:"outputs"`
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

func LoadConfig(r *safeio.Root) (Config, []byte, error) {
	b, err := readOptional(r, configPath)
	if err != nil {
		return Config{}, nil, err
	}
	c := Config{"1.0", []string{"codex"}, []string{}, []Declaration{}}
	if b != nil {
		if err := strictio.Decode(b, &c); err != nil {
			return c, b, errors.New("invalid OMBP project configuration")
		}
	}
	if c.SchemaVersion != "1.0" || len(c.Agents) != 1 || c.Agents[0] != "codex" {
		return c, b, errors.New("unsupported project schema or Agent selection")
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

func declarations(p *model.Profile, c Config) {
	for _, d := range c.Declarations {
		found := false
		for i := range p.Facts {
			f := &p.Facts[i]
			if f.Kind == d.Kind && f.Name == d.Name {
				f.DeclaredValue = d.Value
				f.DeclaredSource = configPath
				f.Conflict = f.Value != "" && f.Value != d.Value
				found = true
			}
		}
		if !found {
			p.Facts = append(p.Facts, model.Fact{Kind: d.Kind, Name: d.Name, DeclaredValue: d.Value, DeclaredSource: configPath, Status: "manual-unverified", Evidence: []model.Evidence{}})
			p.UnknownItems = append(p.UnknownItems, "manual declaration without detected evidence: "+d.Kind+"/"+d.Name)
		}
	}
	sort.Slice(p.Facts, func(i, j int) bool { return p.Facts[i].Kind+"/"+p.Facts[i].Name < p.Facts[j].Kind+"/"+p.Facts[j].Name })
}

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
		if strictio.Decode(b, &lock) != nil || lock.SchemaVersion != "1.0" || len(lock.Outputs) == 0 {
			return lock, b, errors.New("invalid or unsupported OMBP lock")
		}
	}
	return lock, b, nil
}

// Build never writes files and refuses to adopt any unmanaged existing output.
func Build(ctx context.Context, root, version string) (Plan, error) {
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
		return Plan{}, err
	}
	defer r.Close()
	if _, err := r.Lstat(".ombp/state/journal.json"); err == nil {
		return Plan{}, errors.New("unfinished transaction: recover before planning")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Plan{}, err
	}
	c, configBytes, err := LoadConfig(r)
	if err != nil {
		return Plan{}, err
	}
	old, lockBytes, err := LoadLock(r)
	if err != nil {
		return Plan{}, err
	}
	if lockBytes != nil && old.ToolVersion != version {
		return Plan{}, errors.New("tool version differs from locked version; explicit upgrade is required")
	}
	options := scanner.DefaultOptions()
	options.Exclude = c.Exclude
	p, err := scanner.Scan(ctx, abs, options)
	if err != nil {
		return Plan{}, err
	}
	declarations(&p, c)
	extra, err := rules.LoadCustom(abs)
	if err != nil {
		return Plan{}, err
	}
	resolved, err := rules.Resolve(p, extra)
	if err != nil {
		return Plan{}, err
	}
	files, err := renderer.Render(p, resolved)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{"1.0", version, abs, map[string]string{}, []Change{}, ""}
	for name, hash := range p.Inputs {
		plan.Inputs[name] = hash
	}
	plan.Inputs[configPath] = safeio.Digest(configBytes)
	plan.Inputs[lockPath] = safeio.Digest(lockBytes)
	for _, rule := range extra {
		b, e := r.Read(rule.Source.Path, 128<<10)
		if e != nil {
			return Plan{}, e
		}
		plan.Inputs[rule.Source.Path] = safeio.Digest(b)
	}
	outputs := map[string]Output{}
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
			content = string(b[:a]) + newBlock + string(b[z:])
		}
		outputs[f.Path] = Output{safeio.Digest([]byte(content)), safeio.Digest([]byte(newBlock))}
		plan.add(f.Path, b, []byte(content))
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
	newProfile := encode(p)
	if old.ProfileFingerprint == fingerprint && profileBytes != nil {
		newProfile = profileBytes
	}
	plan.add(profilePath, profileBytes, newProfile)
	if configBytes == nil {
		plan.add(configPath, nil, encode(c))
	}
	lock := Lock{"1.0", version, "1.0.0", "1.0.0", fingerprint, safeio.Digest(newProfile), resolved.Fingerprint, []RuleLock{}, outputs}
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
	diff := "--- a/" + name + "\n+++ b/" + name + "\n@@ planned full-file replacement @@\n"
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
	if strictio.Decode(b, &p) != nil || p.SchemaVersion != "1.0" || p.Fingerprint == "" || len(p.Changes) > 128 {
		return p, errors.New("invalid plan schema")
	}
	if planDigest(p) != p.Fingerprint {
		return p, errors.New("plan fingerprint mismatch")
	}
	return p, nil
}

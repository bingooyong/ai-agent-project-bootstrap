// Package rules parses and resolves versioned engineering instructions as data.
package rules

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bingooyong/oh-my-blueprint/internal/strictio"
)

const (
	maxRuleBytes = 128 << 10
	maxRules     = 256
)

var (
	idPattern      = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$`)
)

type Source struct {
	Package  string `json:"package" yaml:"package"`
	Path     string `json:"path,omitempty" yaml:"-"`
	Revision string `json:"revision,omitempty" yaml:"revision,omitempty"`
}

type Scope struct {
	Modules []string `json:"modules" yaml:"modules"`
	Paths   []string `json:"paths" yaml:"paths"`
}

type Conditions struct {
	Languages    []string `json:"languages,omitempty" yaml:"languages,omitempty"`
	Frameworks   []string `json:"frameworks,omitempty" yaml:"frameworks,omitempty"`
	Databases    []string `json:"databases,omitempty" yaml:"databases,omitempty"`
	ProjectTypes []string `json:"project_types,omitempty" yaml:"project_types,omitempty"`
}

type Dependency struct {
	ID      string `json:"id" yaml:"id"`
	Version string `json:"version" yaml:"version"`
}

// Constraint concerns one scalar property. equals/requires fix its value,
// forbids excludes values, and allows limits it to a nonempty scalar set.
type Constraint struct {
	Key      string `json:"key" yaml:"key"`
	Operator string `json:"operator" yaml:"operator"`
	Value    any    `json:"value" yaml:"value"`
}

type Validation struct {
	Kind        string `json:"kind" yaml:"kind"`
	Description string `json:"description" yaml:"description"`
}

type Rule struct {
	SchemaVersion string       `json:"schema_version" yaml:"schema_version"`
	ID            string       `json:"id" yaml:"id"`
	Title         string       `json:"title" yaml:"title"`
	Version       string       `json:"version" yaml:"version"`
	Category      string       `json:"category" yaml:"category"`
	Severity      string       `json:"severity" yaml:"severity"`
	Source        Source       `json:"source" yaml:"source"`
	Origins       []Source     `json:"origins,omitempty" yaml:"-"`
	Scope         Scope        `json:"scope" yaml:"scope"`
	When          Conditions   `json:"when" yaml:"when"`
	Overridable   bool         `json:"overridable" yaml:"overridable"`
	Dependencies  []Dependency `json:"dependencies" yaml:"dependencies"`
	Conflicts     []string     `json:"conflicts" yaml:"conflicts"`
	Constraints   []Constraint `json:"constraints" yaml:"constraints"`
	Enforcement   []string     `json:"enforcement" yaml:"enforcement"`
	Validation    Validation   `json:"validation" yaml:"validation"`
	Content       string       `json:"content" yaml:"-"`
	SHA256        string       `json:"sha256" yaml:"-"`
}

type Resolution struct {
	Rules            []Rule              `json:"rules" yaml:"rules"`
	Fingerprint      string              `json:"fingerprint" yaml:"fingerprint"`
	Warnings         []string            `json:"warnings" yaml:"warnings"`
	SelectionReasons map[string][]string `json:"selection_reasons" yaml:"selection_reasons"`
	Overrides        []Override          `json:"overrides,omitempty" yaml:"overrides,omitempty"`
}

// Parse accepts strictly typed YAML front matter and a nonempty Markdown body.
// No template functions or project code are evaluated.
func Parse(data []byte) (Rule, error) {
	if len(data) > maxRuleBytes || !utf8.Valid(data) {
		return Rule{}, errors.New("rule exceeds size limit or is not UTF-8")
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.ContainsRune(text, '\r') || !strings.HasPrefix(text, "---\n") {
		return Rule{}, errors.New("rule requires YAML front matter delimited by ---")
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return Rule{}, errors.New("rule front matter is not closed")
	}
	end += 4
	var r Rule
	if err := strictio.Decode([]byte(text[4:end]), &r); err != nil {
		// Decoder diagnostics can quote values from untrusted metadata. Keep
		// parser details out of reports rather than echoing a credential value.
		return Rule{}, errors.New("rule front matter violates strict schema")
	}
	r.Content = text[end+5:]
	return normalize(r)
}

func normalize(r Rule) (Rule, error) {
	// Provenance is established by the loader/resolver, never by rule text.
	r.Origins = nil
	if r.SchemaVersion != "1.0" || !validID(r.ID) || !versionPattern.MatchString(r.Version) {
		return Rule{}, errors.New("rule schema, ID or exact version is invalid")
	}
	if strings.TrimSpace(r.Title) == "" || len(r.Title) > 256 || hasControl(r.Title) || !validID(r.Category) || !validID(r.Source.Package) {
		return Rule{}, fmt.Errorf("rule %s has invalid title, category or source", r.ID)
	}
	if !slices.Contains([]string{"P0", "P1", "P2", "P3"}, r.Severity) {
		return Rule{}, fmt.Errorf("rule %s has invalid severity", r.ID)
	}
	if (r.Severity == "P0" || r.Severity == "P1") && r.Overridable {
		return Rule{}, fmt.Errorf("protected rule %s cannot be overridable in this version", r.ID)
	}
	scope, err := normalizeScope(r.Scope)
	if err != nil {
		return Rule{}, fmt.Errorf("rule %s: %w", r.ID, err)
	}
	r.Scope = scope
	for _, values := range [][]string{r.When.Languages, r.When.Frameworks, r.When.Databases, r.When.ProjectTypes} {
		for _, v := range values {
			if v != "*" && !validID(v) {
				return Rule{}, fmt.Errorf("rule %s has invalid condition value", r.ID)
			}
		}
	}
	r.When.Languages = sortedUnique(r.When.Languages)
	r.When.Frameworks = sortedUnique(r.When.Frameworks)
	r.When.Databases = sortedUnique(r.When.Databases)
	r.When.ProjectTypes = sortedUnique(r.When.ProjectTypes)
	if len(r.Dependencies)+len(r.Conflicts)+len(r.Constraints) > 128 {
		return Rule{}, errors.New("rule relationship budget exceeded")
	}
	r.Dependencies = slices.Clone(r.Dependencies)
	if r.Dependencies == nil {
		r.Dependencies = []Dependency{}
	}
	slices.SortFunc(r.Dependencies, func(a, b Dependency) int { return strings.Compare(a.ID, b.ID) })
	for i, d := range r.Dependencies {
		if !validID(d.ID) || !versionPattern.MatchString(d.Version) || (i > 0 && r.Dependencies[i-1].ID == d.ID) {
			return Rule{}, fmt.Errorf("rule %s has invalid or duplicate dependency", r.ID)
		}
	}
	for _, id := range r.Conflicts {
		if !validID(id) {
			return Rule{}, fmt.Errorf("rule %s has invalid conflict ID", r.ID)
		}
	}
	r.Conflicts = sortedUnique(r.Conflicts)
	if r.Conflicts == nil {
		r.Conflicts = []string{}
	}
	r.Constraints = slices.Clone(r.Constraints)
	if r.Constraints == nil {
		r.Constraints = []Constraint{}
	}
	for i := range r.Constraints {
		c := &r.Constraints[i]
		if !validID(c.Key) || !slices.Contains([]string{"equals", "requires", "forbids", "allows"}, c.Operator) {
			return Rule{}, fmt.Errorf("rule %s has unsupported constraint", r.ID)
		}
		v, err := normalizeConstraintValue(c.Operator, c.Value)
		if err != nil {
			return Rule{}, fmt.Errorf("rule %s constraint %s: %w", r.ID, c.Key, err)
		}
		c.Value = v
	}
	slices.SortFunc(r.Constraints, func(a, b Constraint) int {
		return bytes.Compare(jsonBytes(a), jsonBytes(b))
	})
	if len(r.Enforcement) == 0 {
		return Rule{}, fmt.Errorf("rule %s requires an enforcement description", r.ID)
	}
	for _, e := range r.Enforcement {
		if !slices.Contains([]string{"instruction", "static", "manual-review", "ci"}, e) {
			return Rule{}, fmt.Errorf("rule %s has unknown enforcement", r.ID)
		}
	}
	r.Enforcement = sortedUnique(r.Enforcement)
	if !slices.Contains([]string{"manual", "static"}, r.Validation.Kind) || strings.TrimSpace(r.Validation.Description) == "" || len(r.Validation.Description) > 1024 || hasControl(r.Validation.Description) {
		return Rule{}, fmt.Errorf("rule %s requires nonexecutable validation metadata", r.ID)
	}
	r.Content = strings.TrimSpace(strings.ReplaceAll(r.Content, "\r\n", "\n")) + "\n"
	if strings.TrimSpace(r.Content) == "" || len(r.Content) > maxRuleBytes || strings.Contains(r.Content, "<!-- ombp:") || strings.ContainsRune(r.Content, '\x00') || strings.ContainsRune(r.Content, '\r') {
		return Rule{}, fmt.Errorf("rule %s has empty or unsafe body", r.ID)
	}
	canonical := r
	canonical.SHA256 = ""
	canonical.Source.Path = ""
	r.SHA256 = digest(jsonBytes(canonical))
	return r, nil
}

func normalizeScope(scope Scope) (Scope, error) {
	for _, patterns := range [][]string{scope.Modules, scope.Paths} {
		if len(patterns) == 0 {
			return Scope{}, errors.New("explicit scope is required")
		}
		if len(patterns) > 16 {
			return Scope{}, errors.New("scope pattern budget exceeded")
		}
		for _, p := range patterns {
			if err := validateGlob(p); err != nil {
				return Scope{}, err
			}
		}
	}
	return Scope{Modules: sortedUnique(scope.Modules), Paths: sortedUnique(scope.Paths)}, nil
}

func normalizeConstraintValue(operator string, value any) (any, error) {
	if operator != "allows" && operator != "forbids" {
		if !scalar(value) {
			return nil, errors.New("equals/requires must have a string, boolean or integer value")
		}
		return value, nil
	}
	var values []any
	switch v := value.(type) {
	case []any:
		values = slices.Clone(v)
	case []string:
		for _, item := range v {
			values = append(values, item)
		}
	default:
		if operator == "forbids" && scalar(value) {
			return value, nil
		}
		return nil, errors.New("allows must be a nonempty scalar set")
	}
	if len(values) == 0 || len(values) > 64 {
		return nil, errors.New("constraint set size is invalid")
	}
	for _, v := range values {
		if !scalar(v) {
			return nil, errors.New("constraint set contains a nonscalar value")
		}
	}
	slices.SortFunc(values, func(a, b any) int { return bytes.Compare(jsonBytes(a), jsonBytes(b)) })
	return slices.CompactFunc(values, func(a, b any) bool { return bytes.Equal(jsonBytes(a), jsonBytes(b)) }), nil
}

func scalar(v any) bool {
	switch n := v.(type) {
	case string:
		return len(n) <= 256 && !hasControl(n)
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	default:
		return false
	}
}

func validID(s string) bool    { return len(s) <= 128 && idPattern.MatchString(s) }
func hasControl(s string) bool { return strings.IndexFunc(s, unicode.IsControl) >= 0 }
func sortedUnique(v []string) []string {
	out := slices.Clone(v)
	slices.Sort(out)
	return slices.Compact(out)
}
func jsonBytes(v any) []byte { data, _ := json.Marshal(v); return data }
func digest(v []byte) string { h := sha256.Sum256(v); return hex.EncodeToString(h[:]) }

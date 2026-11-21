package rules

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bingooyong/oh-my-blueprint/internal/model"
)

// Resolve includes built-in rules and explicitly supplied custom rules. It never
// reads project files, downloads packages, or evaluates natural-language text.
func Resolve(profile model.Profile, extra []Rule) (Resolution, error) {
	return ResolveWithOverrides(profile, extra, nil)
}

// ResolveWithOverrides validates exact local replacement records before
// selecting rules and resolving their relationships.
func ResolveWithOverrides(profile model.Profile, extra []Rule, records []Override) (Resolution, error) {
	builtin, err := Builtin()
	if err != nil {
		return Resolution{}, err
	}
	all := append(builtin, extra...)
	if len(all) > maxRules || len(records) > maxRules {
		return Resolution{}, errors.New("rule count budget exceeded")
	}
	definitions := make(map[string]map[string]Rule, len(all))
	for _, candidate := range all {
		r, err := normalize(candidate)
		if err != nil {
			return Resolution{}, err
		}
		versions := definitions[r.ID]
		if versions == nil {
			versions = make(map[string]Rule)
			definitions[r.ID] = versions
		}
		if old, exists := versions[r.Version]; exists {
			if old.SHA256 != r.SHA256 {
				return Resolution{}, fmt.Errorf("conflicting definitions for rule %s at the same version", r.ID)
			}
			old.Origins = append(old.Origins, r.Source)
			slices.SortFunc(old.Origins, func(a, b Source) int { return strings.Compare(string(jsonBytes(a)), string(jsonBytes(b))) })
			old.Origins = slices.Compact(old.Origins)
			old.Source = old.Origins[0]
			versions[r.Version] = old
			continue
		}
		r.Origins = []Source{r.Source}
		versions[r.Version] = r
	}
	overrides := make(map[string]Override, len(records))
	for _, record := range records {
		o, err := normalizeOverride(record)
		if err != nil {
			return Resolution{}, err
		}
		if _, exists := overrides[o.RuleID]; exists {
			return Resolution{}, fmt.Errorf("rule %s may have only one override record", o.RuleID)
		}
		if _, exists := definitions[o.RuleID]; !exists {
			return Resolution{}, fmt.Errorf("override target rule %s is unavailable", o.RuleID)
		}
		overrides[o.RuleID] = o
	}
	ids := make([]string, 0, len(definitions))
	for id := range definitions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	registry := make(map[string]Rule, len(definitions))
	for _, id := range ids {
		versions := definitions[id]
		if record, exists := overrides[id]; exists {
			replacement, err := applyOverride(record, versions)
			if err != nil {
				return Resolution{}, err
			}
			registry[id] = replacement
			continue
		}
		if len(versions) != 1 {
			return Resolution{}, fmt.Errorf("conflicting definitions for rule %s; explicit override is required and protected rules cannot be weakened", id)
		}
		for _, rule := range versions {
			registry[id] = rule
		}
	}
	state := map[string]byte{}
	reasons := map[string][]string{}
	var ordered []Rule
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 2 {
			return nil
		}
		if state[id] == 1 {
			return fmt.Errorf("rule dependency cycle at %s", id)
		}
		r := registry[id]
		state[id] = 1
		for _, dep := range r.Dependencies {
			d, exists := registry[dep.ID]
			if !exists || d.Version != dep.Version {
				return fmt.Errorf("rule %s requires unavailable exact dependency %s@%s", id, dep.ID, dep.Version)
			}
			if !matches(profile, d.When) {
				return fmt.Errorf("rule %s depends on %s whose applicability is not established", id, d.ID)
			}
			reasons[dep.ID] = append(reasons[dep.ID], "dependency of "+id+"@"+r.Version)
			if err := visit(dep.ID); err != nil {
				return err
			}
		}
		state[id] = 2
		ordered = append(ordered, r)
		return nil
	}
	var applied []Override
	for _, id := range ids {
		if matches(profile, registry[id].When) {
			reasons[id] = append(reasons[id], selectionReason(profile, registry[id]))
			if record, exists := overrides[id]; exists {
				applied = append(applied, record)
				reasons[id] = append(reasons[id], "selected explicit replacement of "+id+"@"+record.Before.Version)
			}
			if err := visit(id); err != nil {
				return Resolution{}, err
			}
		}
	}
	analysis := &scopeAnalyzer{budget: 2_000_000, cache: map[string]scopeResult{}}
	for i, a := range ordered {
		if slices.Contains(a.Conflicts, a.ID) {
			return Resolution{}, fmt.Errorf("rule %s conflicts with itself", a.ID)
		}
		if !constraintsSatisfiable(a.Constraints) {
			return Resolution{}, fmt.Errorf("rule %s contains contradictory constraints", a.ID)
		}
		for _, b := range ordered[i+1:] {
			explicit := slices.Contains(a.Conflicts, b.ID) || slices.Contains(b.Conflicts, a.ID)
			constraints := sharedConstraints(a.Constraints, b.Constraints)
			if !explicit && len(constraints) == 0 {
				continue
			}
			overlap, known := analysis.overlap(a.Scope, b.Scope)
			if !known {
				return Resolution{}, fmt.Errorf("scope-overlap-unknown between %s and %s", a.ID, b.ID)
			}
			if !overlap {
				continue
			}
			if explicit || !constraintsSatisfiable(constraints) {
				return Resolution{}, fmt.Errorf("rules %s and %s conflict in overlapping scope", a.ID, b.ID)
			}
		}
	}
	// Pair checks alone miss a finite allowed set exhausted by three or more
	// rules. A shared path must belong to a clique in the scope-overlap graph.
	// Search those cliques conservatively: pairwise overlap does not establish a
	// common witness for every possible glob, so a failure is a review blocker,
	// never a claim of complete natural-language or scope reasoning.
	if err := checkConstraintCombinations(ordered, analysis); err != nil {
		return Resolution{}, err
	}
	warnings := []string{"Natural-language rule conflicts require human review; deterministic checks cover metadata and typed constraints only."}
	for _, r := range ordered {
		for _, c := range r.Conflicts {
			if _, exists := registry[c]; !exists {
				warnings = append(warnings, fmt.Sprintf("rule %s names unavailable conflict target %s", r.ID, c))
			}
		}
	}
	warnings = sortedUnique(warnings)
	for id, values := range reasons {
		reasons[id] = sortedUnique(values)
	}
	manifest := struct {
		Rules     []Rule              `json:"rules"`
		Reasons   map[string][]string `json:"selection_reasons"`
		Overrides []Override          `json:"overrides,omitempty"`
	}{ordered, reasons, applied}
	return Resolution{Rules: ordered, Fingerprint: digest(jsonBytes(manifest)), Warnings: warnings, SelectionReasons: reasons, Overrides: applied}, nil
}

func selectionReason(profile model.Profile, r Rule) string {
	if len(r.When.Languages)+len(r.When.Frameworks)+len(r.When.Databases)+len(r.When.ProjectTypes) == 0 {
		return "unconditional engineering rule"
	}
	var evidence []string
	for _, selector := range selectors(r.When) {
		for _, fact := range profile.Facts {
			if factMatches(fact, selector.kind, selector.values) {
				for _, source := range fact.Evidence {
					evidence = append(evidence, source.Path+"#"+source.Pointer)
				}
			}
		}
	}
	if len(evidence) == 0 {
		return "universal applicability condition " + string(jsonBytes(r.When))
	}
	return "matched evidence conditions " + string(jsonBytes(r.When)) + "; evidence " + strings.Join(sortedUnique(evidence), ", ")
}

func matches(profile model.Profile, conditions Conditions) bool {
	for _, selector := range selectors(conditions) {
		if len(selector.values) == 0 || slices.Contains(selector.values, "*") {
			continue
		}
		matched := false
		for _, fact := range profile.Facts {
			if factMatches(fact, selector.kind, selector.values) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

type selector struct {
	kind   string
	values []string
}

func selectors(c Conditions) []selector {
	return []selector{{"language", c.Languages}, {"framework", c.Frameworks}, {"database", c.Databases}, {"project_type", c.ProjectTypes}}
}
func factMatches(f model.Fact, kind string, values []string) bool {
	return strings.TrimSuffix(f.Kind, "s") == kind && !f.Conflict && slices.Contains([]string{"declared", "detected", "confirmed"}, f.Status) && f.Confidence > 0 && len(f.Evidence) > 0 && (slices.Contains(values, f.Name) || slices.Contains(values, f.Value))
}

func sharedConstraints(a, b []Constraint) []Constraint {
	keys := map[string]bool{}
	for _, x := range a {
		for _, y := range b {
			if x.Key == y.Key {
				keys[x.Key] = true
			}
		}
	}
	var out []Constraint
	for _, cs := range [][]Constraint{a, b} {
		for _, c := range cs {
			if keys[c.Key] {
				out = append(out, c)
			}
		}
	}
	return out
}

func checkConstraintCombinations(all []Rule, analysis *scopeAnalyzer) error {
	byKey := map[string][]Rule{}
	for _, r := range all {
		keys := map[string]bool{}
		for _, c := range r.Constraints {
			keys[c.Key] = true
		}
		for key := range keys {
			byKey[key] = append(byKey[key], r)
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	budget := 10000
	for _, key := range keys {
		group := byKey[key]
		constraints := make([][]Constraint, len(group))
		var combined []Constraint
		for i, r := range group {
			for _, c := range r.Constraints {
				if c.Key == key {
					constraints[i] = append(constraints[i], c)
					combined = append(combined, c)
				}
			}
		}
		if constraintsSatisfiable(combined) {
			continue
		}
		edges := make([][]bool, len(group))
		for i := range group {
			edges[i] = make([]bool, len(group))
			for j := range group {
				yes, known := analysis.overlap(group[i].Scope, group[j].Scope)
				if !known {
					return errors.New("scope-overlap-unknown during constraint combination analysis")
				}
				edges[i][j] = yes
			}
		}
		var search func([]int, []int, []Constraint) error
		search = func(selected, candidates []int, accumulated []Constraint) error {
			budget--
			if budget < 0 {
				return errors.New("constraint scope analysis budget exceeded; review required")
			}
			if !constraintsSatisfiable(accumulated) {
				ids := make([]string, len(selected))
				for i, index := range selected {
					ids[i] = group[index].ID
				}
				return fmt.Errorf("constraint %s may conflict across mutually overlapping rules %s; review required", key, strings.Join(ids, ", "))
			}
			possible := slices.Clone(accumulated)
			for _, index := range candidates {
				possible = append(possible, constraints[index]...)
			}
			if constraintsSatisfiable(possible) {
				return nil
			}
			for pos, index := range candidates {
				next := make([]int, 0, len(candidates)-pos-1)
				for _, other := range candidates[pos+1:] {
					if edges[index][other] {
						next = append(next, other)
					}
				}
				if err := search(append(slices.Clone(selected), index), next, append(slices.Clone(accumulated), constraints[index]...)); err != nil {
					return err
				}
			}
			return nil
		}
		candidates := make([]int, len(group))
		for i := range candidates {
			candidates[i] = i
		}
		if err := search(nil, candidates, nil); err != nil {
			return err
		}
	}
	return nil
}

func constraintsSatisfiable(constraints []Constraint) bool {
	type domain struct {
		required    string
		hasRequired bool
		allowed     map[string]bool
		forbidden   map[string]bool
	}
	domains := map[string]*domain{}
	for _, c := range constraints {
		d := domains[c.Key]
		if d == nil {
			d = &domain{forbidden: map[string]bool{}}
			domains[c.Key] = d
		}
		key := string(jsonBytes(c.Value))
		switch c.Operator {
		case "equals", "requires":
			if d.hasRequired && d.required != key {
				return false
			}
			d.required = key
			d.hasRequired = true
		case "forbids":
			for _, v := range scalarSet(c.Value) {
				d.forbidden[string(jsonBytes(v))] = true
			}
		case "allows":
			set := map[string]bool{}
			for _, v := range scalarSet(c.Value) {
				k := string(jsonBytes(v))
				if d.allowed == nil || d.allowed[k] {
					set[k] = true
				}
			}
			d.allowed = set
		}
	}
	for _, d := range domains {
		if d.hasRequired && (d.forbidden[d.required] || (d.allowed != nil && !d.allowed[d.required])) {
			return false
		}
		if d.allowed != nil {
			any := false
			for v := range d.allowed {
				if !d.forbidden[v] {
					any = true
				}
			}
			if !any {
				return false
			}
		}
	}
	return true
}

func scalarSet(v any) []any {
	if values, ok := v.([]any); ok {
		return values
	}
	return []any{v}
}

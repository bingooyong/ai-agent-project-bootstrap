package rules

import (
	"testing"

	"github.com/bingooyong/oh-my-blueprint/internal/model"
)

func TestResolveExplicitConflictsRespectScope(t *testing.T) {
	for _, overlap := range []bool{true, false} {
		name := "disjoint"
		if overlap {
			name = "overlapping"
		}
		t.Run(name, func(t *testing.T) {
			a, b := customRule(t, "project.a"), customRule(t, "project.b")
			a.Conflicts = []string{b.ID}
			a.Scope.Paths = []string{"backend/**"}
			b.Scope.Paths = []string{"frontend/**"}
			if overlap {
				b.Scope.Paths = []string{"backend/*.go"}
			}
			_, err := Resolve(model.Profile{}, []Rule{a, b})
			if (err != nil) != overlap {
				t.Fatalf("overlap=%v error=%v", overlap, err)
			}
		})
	}
}

func TestResolveTypedConstraints(t *testing.T) {
	cases := []struct {
		name        string
		constraints []Constraint
		wantError   bool
	}{
		{"same_required", []Constraint{{"property.mode", "requires", "safe"}, {"property.mode", "equals", "safe"}}, false},
		{"different_required", []Constraint{{"property.mode", "requires", "safe"}, {"property.mode", "equals", "unsafe"}}, true},
		{"required_forbidden", []Constraint{{"property.mode", "requires", "safe"}, {"property.mode", "forbids", "safe"}}, true},
		{"required_not_allowed", []Constraint{{"property.mode", "requires", "safe"}, {"property.mode", "allows", []string{"unsafe"}}}, true},
		{"allowed_intersection", []Constraint{{"property.mode", "allows", []string{"a", "b"}}, {"property.mode", "allows", []string{"b", "c"}}}, false},
		{"disjoint_allowed", []Constraint{{"property.mode", "allows", []string{"a"}}, {"property.mode", "allows", []string{"b"}}}, true},
		{"set_exhausted_across_three", []Constraint{{"property.mode", "allows", []string{"a", "b"}}, {"property.mode", "forbids", "a"}, {"property.mode", "forbids", "b"}}, true},
		{"typed_values", []Constraint{{"property.mode", "requires", true}, {"property.mode", "equals", "true"}}, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			extra := make([]Rule, 0, len(tt.constraints))
			for i, c := range tt.constraints {
				r := customRule(t, "project."+string(rune('a'+i)))
				r.Constraints = []Constraint{c}
				extra = append(extra, r)
			}
			_, err := Resolve(model.Profile{}, extra)
			if (err != nil) != tt.wantError {
				t.Fatalf("error=%v wantError=%v", err, tt.wantError)
			}
		})
	}
}

func TestResolveContradictoryLocalRuleAndProtectedConstraint(t *testing.T) {
	for _, name := range []string{"local", "protected"} {
		t.Run(name, func(t *testing.T) {
			r := customRule(t, "project.custom")
			if name == "local" {
				r.Constraints = []Constraint{{"safety.auth", "requires", true}, {"safety.auth", "forbids", true}}
			} else {
				r.Constraints = []Constraint{{"operations.production.authorization", "equals", "none"}}
			}
			if _, err := Resolve(model.Profile{}, []Rule{r}); err == nil {
				t.Fatal("accepted contradictory constraint")
			}
		})
	}
}

func TestResolveConstraintCombinationsRespectNestedScopes(t *testing.T) {
	for _, name := range []string{"nested_overlap", "disjoint_exclusions"} {
		t.Run(name, func(t *testing.T) {
			a, b, c := customRule(t, "project.a"), customRule(t, "project.b"), customRule(t, "project.c")
			a.Constraints = []Constraint{{"property.mode", "allows", []string{"a", "b"}}}
			b.Scope.Paths = []string{"backend/**"}
			b.Constraints = []Constraint{{"property.mode", "forbids", "a"}}
			c.Scope.Paths = []string{"backend/*.go"}
			c.Constraints = []Constraint{{"property.mode", "forbids", "b"}}
			if name == "disjoint_exclusions" {
				c.Scope.Paths = []string{"frontend/**"}
			}
			_, err := Resolve(model.Profile{}, []Rule{a, b, c})
			if (err != nil) != (name == "nested_overlap") {
				t.Fatalf("unexpected combination result: %v", err)
			}
		})
	}
}

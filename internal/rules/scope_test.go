package rules

import "testing"

func TestGlobOverlap(t *testing.T) {
	for _, tt := range []struct {
		name, a, b string
		want       bool
	}{
		{"root_file", "**/*", "go.mod", true},
		{"nested_file", "**/*.go", "backend/a.go", true},
		{"different_extensions", "**/*.go", "**/*.py", false},
		{"different_roots", "backend/**", "frontend/**", false},
		{"same_root", "backend/**", "backend/*.go", true},
		{"zero_directories", "a/**/b", "a/b", true},
		{"several_directories", "a/**/b", "a/x/y/b", true},
		{"directory_not_arbitrary_prefix", "a/**/b", "a/xb", false},
		{"single_star_no_slash", "a/*", "a/x/y", false},
		{"question_no_slash", "?", "/", false},
		{"different_exact", "foo", "foo/bar", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, known := globOverlap(tt.a, tt.b)
			if !known || got != tt.want {
				t.Fatalf("%q %q: overlap=%v known=%v", tt.a, tt.b, got, known)
			}
			other, k := globOverlap(tt.b, tt.a)
			if !k || other != got {
				t.Fatal("intersection is asymmetric")
			}
		})
	}
}

func TestScopeOverlap(t *testing.T) {
	a := Scope{Modules: []string{"*"}, Paths: []string{"*"}}
	b := Scope{Modules: []string{"backend/**"}, Paths: []string{"nested/file"}}
	if overlap, known := scopeOverlap(a, b); !known || overlap {
		t.Fatal("path * must not be promoted to **")
	}
	a.Paths = []string{"**/*"}
	if overlap, known := scopeOverlap(a, b); !known || !overlap {
		t.Fatal("module wildcard must include nested modules")
	}
}

func TestScopeAnalysisBudget(t *testing.T) {
	budget := 0
	if _, known := globOverlapBudget("a*", "ab", &budget); known {
		t.Fatal("exhausted budget must remain unknown")
	}
	a := &scopeAnalyzer{budget: 100, cache: map[string]scopeResult{}}
	scope := Scope{Modules: []string{"*"}, Paths: []string{"**/*"}}
	if yes, known := a.overlap(scope, scope); !yes || !known {
		t.Fatal("expected overlap")
	}
	a.budget = 0
	if yes, known := a.overlap(scope, scope); !yes || !known {
		t.Fatal("cached proven result should remain available")
	}
}

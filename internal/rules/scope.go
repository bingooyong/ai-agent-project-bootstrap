package rules

import (
	"errors"
	"strings"
	"unicode"
)

func validateGlob(p string) error {
	if len(p) == 0 || len(p) > 128 || strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") || strings.ContainsAny(p, `\:[]{}()!`) || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return errors.New("unsupported or unsafe scope glob")
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == ".." {
			return errors.New("scope path cannot escape or contain empty components")
		}
	}
	return nil
}

type globToken struct {
	kind byte
	char rune
}

// tokens compiles the supported path-glob subset to a tiny nondeterministic
// automaton. A **/ token can skip zero directories or consume through a slash.
func tokens(pattern string) []globToken {
	r := []rune(pattern)
	var out []globToken
	for i := 0; i < len(r); i++ {
		switch r[i] {
		case '*':
			if i+1 < len(r) && r[i+1] == '*' {
				i++
				if i+1 < len(r) && r[i+1] == '/' {
					i++
					out = append(out, globToken{kind: 'd'}, globToken{kind: 'e'})
				} else {
					out = append(out, globToken{kind: 'a'})
				}
			} else {
				out = append(out, globToken{kind: 's'})
			}
		case '?':
			out = append(out, globToken{kind: 'q'})
		default:
			out = append(out, globToken{kind: 'l', char: r[i]})
		}
	}
	return out
}

type edge struct {
	to   int
	kind byte
	char rune
}

func transitions(t []globToken, i int) []edge {
	if i == len(t) {
		return nil
	}
	v := t[i]
	switch v.kind {
	case 's':
		return []edge{{to: i, kind: 'q'}}
	case 'a':
		return []edge{{to: i, kind: 'a'}}
	case 'd':
		return []edge{{to: i + 1, kind: 'a'}, {to: i + 2, kind: 'l', char: '/'}}
	case 'e':
		return []edge{{to: i, kind: 'a'}, {to: i + 1, kind: 'l', char: '/'}}
	default:
		return []edge{{to: i + 1, kind: v.kind, char: v.char}}
	}
}
func epsilon(t []globToken, i int) int {
	if i < len(t) {
		if t[i].kind == 'd' {
			return i + 2
		}
		if t[i].kind == 's' || t[i].kind == 'a' {
			return i + 1
		}
	}
	return -1
}
func intersect(a, b edge) bool {
	if a.kind == 'l' && b.kind == 'l' {
		return a.char == b.char
	}
	if a.kind == 'l' {
		return b.kind == 'a' || a.char != '/'
	}
	if b.kind == 'l' {
		return a.kind == 'a' || b.char != '/'
	}
	return true
}

// globOverlap returns known=false if the bounded automaton search is exhausted.
func globOverlap(a, b string) (overlap, known bool) {
	budget := 16384
	return globOverlapBudget(a, b, &budget)
}

func globOverlapBudget(a, b string, budget *int) (overlap, known bool) {
	ta, tb := tokens(a), tokens(b)
	type pair struct{ a, b int }
	queue := []pair{{}}
	seen := map[pair]bool{}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if seen[p] {
			continue
		}
		seen[p] = true
		*budget--
		if len(seen) > 16384 || *budget < 0 {
			return false, false
		}
		if p.a == len(ta) && p.b == len(tb) {
			return true, true
		}
		if next := epsilon(ta, p.a); next >= 0 {
			queue = append(queue, pair{next, p.b})
		}
		if next := epsilon(tb, p.b); next >= 0 {
			queue = append(queue, pair{p.a, next})
		}
		for _, ea := range transitions(ta, p.a) {
			for _, eb := range transitions(tb, p.b) {
				if intersect(ea, eb) {
					queue = append(queue, pair{ea.to, eb.to})
				}
			}
		}
	}
	return false, true
}

func scopeOverlap(a, b Scope) (bool, bool) {
	budget := 65536
	return scopeOverlapBudget(a, b, &budget)
}

func scopeOverlapBudget(a, b Scope, budget *int) (bool, bool) {
	for dimension, dimensions := range [][2][]string{{a.Modules, b.Modules}, {a.Paths, b.Paths}} {
		anyOverlap, unknown := false, false
	search:
		for _, p := range dimensions[0] {
			for _, q := range dimensions[1] {
				// The module wildcard denotes every module including nested paths.
				if dimension == 0 {
					if p == "*" {
						p = "**"
					}
					if q == "*" {
						q = "**"
					}
				}
				yes, known := globOverlapBudget(p, q, budget)
				anyOverlap = anyOverlap || yes
				unknown = unknown || !known
				if yes {
					break search
				}
			}
		}
		if !anyOverlap {
			if unknown {
				return false, false
			}
			return false, true
		}
	}
	return true, true
}

type scopeResult struct{ overlap, known bool }
type scopeAnalyzer struct {
	budget int
	cache  map[string]scopeResult
}

func (a *scopeAnalyzer) overlap(x, y Scope) (bool, bool) {
	left, right := string(jsonBytes(x)), string(jsonBytes(y))
	if left > right {
		left, right = right, left
	}
	key := left + "\n" + right
	if got, ok := a.cache[key]; ok {
		return got.overlap, got.known
	}
	yes, known := scopeOverlapBudget(x, y, &a.budget)
	a.cache[key] = scopeResult{yes, known}
	return yes, known
}

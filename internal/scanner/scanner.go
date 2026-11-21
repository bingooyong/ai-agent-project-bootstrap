// Package scanner discovers project declarations without executing project code.
package scanner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/bingooyong/oh-my-blueprint/internal/model"
	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
)

type Options struct {
	MaxFiles     int
	MaxBytes     int64
	MaxFileBytes int64
	MaxDepth     int
	Exclude      []string
	Deep         bool
}

func DefaultOptions() Options {
	return Options{MaxFiles: 20000, MaxBytes: 16 << 20, MaxFileBytes: 1 << 20, MaxDepth: 24}
}

type scan struct {
	ctx     context.Context
	root    *safeio.Root
	options Options
	profile model.Profile
	files   map[string]fs.FileInfo
	data    map[string][]byte
}

// Scan returns a deterministic static profile. A non-nil error means coverage
// is incomplete; callers must not apply rules based on that profile.
func Scan(ctx context.Context, root string, options Options) (model.Profile, error) {
	s := &scan{ctx: ctx, options: options, files: map[string]fs.FileInfo{}, data: map[string][]byte{}}
	s.profile = model.Profile{
		SchemaVersion: "1.0", Facts: []model.Fact{}, Commands: []model.Command{}, Modules: []model.Module{},
		AgentConfigs: []string{}, UnknownItems: []string{}, Inputs: map[string]string{},
		Coverage: model.Coverage{Status: "incomplete", Excluded: []string{}},
	}
	if ctx == nil {
		return s.profile, errors.New("scan context is required")
	}
	if options.MaxFiles <= 0 || options.MaxBytes <= 0 || options.MaxFileBytes <= 0 || options.MaxDepth <= 0 {
		return s.profile, errors.New("scan budgets must be positive")
	}
	if err := ValidateExclusions(options.Exclude); err != nil {
		return s.profile, err
	}
	r, err := safeio.Open(root)
	if err != nil {
		return s.profile, errors.New("cannot open project root safely")
	}
	s.root = r
	defer r.Close()
	err = s.walk(".", 0)
	if err == nil {
		err = s.detect()
	}
	if err == nil {
		s.profile.Coverage.Status = "complete"
	}
	s.finish()
	return s.profile, err
}

// ValidateExclusions checks configured patterns before scanning project files.
func ValidateExclusions(patterns []string) error {
	for _, pattern := range patterns {
		if pattern == "" || len(pattern) > 256 || strings.HasPrefix(pattern, "/") || strings.Contains(pattern, "\\") || strings.Contains(pattern, "..") {
			return errors.New("invalid scan exclusion pattern")
		}
		if _, err := path.Match(strings.ReplaceAll(pattern, "**", "*"), ""); err != nil {
			return errors.New("invalid scan exclusion pattern")
		}
	}
	return nil
}

func (s *scan) walk(dir string, depth int) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	remaining := s.options.MaxFiles - s.profile.Coverage.FilesVisited
	// A one-entry read at zero remaining distinguishes an empty directory
	// from a budget violation without unbounded enumeration.
	entries, err := s.root.ReadDirLimit(dir, min(20001, max(1, remaining+1)))
	if err != nil {
		return fmt.Errorf("cannot enumerate project directory %q", dir)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		name := path.Join(dir, entry.Name())
		// Product-owned control state must not alter the project fingerprint
		// after initialization. Its paths and contents are outside this scan.
		if entry.Name() == ".ombp" {
			continue
		}
		s.profile.Coverage.FilesVisited++
		if s.profile.Coverage.FilesVisited > s.options.MaxFiles {
			return errors.New("project entry budget exceeded")
		}
		if s.excluded(name, entry.Name()) || safeio.Sensitive(name) {
			s.profile.Coverage.Excluded = append(s.profile.Coverage.Excluded, name)
			continue
		}
		info, err := s.root.Lstat(name)
		if err != nil {
			return fmt.Errorf("cannot inspect project path %q", name)
		}
		if info.Mode()&fs.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			s.profile.Coverage.Excluded = append(s.profile.Coverage.Excluded, name)
			continue
		}
		if info.IsDir() {
			if depth >= s.options.MaxDepth {
				return errors.New("project depth budget exceeded")
			}
			if err := s.walk(name, depth+1); err != nil {
				return err
			}
			continue
		}
		s.files[name] = info
	}
	return nil
}

func (s *scan) excluded(name, base string) bool {
	switch base {
	case ".git", ".hg", ".svn", "node_modules", "vendor", "dist", "build", "target", ".venv", "venv", "__pycache__", ".next", ".nuxt", ".ombp", ".trellis", ".DS_Store":
		return true
	}
	for _, pattern := range s.options.Exclude {
		if matchGlob(pattern, name) || matchGlob(pattern, base) {
			return true
		}
	}
	return false
}

// matchGlob supports segment wildcards plus ** without delegating to a shell.
func matchGlob(pattern, name string) bool {
	p, n := strings.Split(pattern, "/"), strings.Split(name, "/")
	type state struct{ pattern, name int }
	known, results := map[state]bool{}, map[state]bool{}
	var match func(int, int) bool
	match = func(i, j int) bool {
		key := state{i, j}
		if known[key] {
			return results[key]
		}
		known[key] = true
		result := false
		defer func() { results[key] = result }()
		if i == len(p) {
			result = j == len(n)
			return result
		}
		if p[i] == "**" {
			result = match(i+1, j) || (j < len(n) && match(i, j+1))
			return result
		}
		if j == len(n) {
			return false
		}
		ok, _ := path.Match(p[i], n[j])
		result = ok && match(i+1, j+1)
		return result
	}
	return match(0, 0)
}

func (s *scan) read(name string) ([]byte, error) {
	if data, ok := s.data[name]; ok {
		return data, nil
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	info, ok := s.files[name]
	if !ok || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("project declaration %q is unavailable", name)
	}
	if info.Size() > s.options.MaxFileBytes || info.Size() > s.options.MaxBytes-s.profile.Coverage.BytesRead {
		return nil, fmt.Errorf("read budget exceeded for declaration %q", name)
	}
	remaining := min(s.options.MaxFileBytes, s.options.MaxBytes-s.profile.Coverage.BytesRead)
	data, err := s.root.Read(name, remaining)
	if err != nil {
		return nil, fmt.Errorf("cannot read declaration %q safely", name)
	}
	s.profile.Coverage.BytesRead += int64(len(data))
	s.data[name] = data
	s.profile.Inputs[name] = safeio.Digest(data)
	return data, nil
}

func (s *scan) evidence(name, pointer string) []model.Evidence {
	return []model.Evidence{{Path: name, Pointer: pointer, SHA256: s.profile.Inputs[name]}}
}

func (s *scan) fact(kind, name, value, file, pointer string) {
	s.profile.Facts = append(s.profile.Facts, model.Fact{
		Kind: kind, Name: name, Value: value, Confidence: 1, Status: "declared", Evidence: s.evidence(file, pointer),
	})
}

func (s *scan) command(name string, argv []string, cwd, file, pointer, status string) {
	s.profile.Commands = append(s.profile.Commands, model.Command{
		Name: name, Argv: argv, Cwd: cwd, Status: status, ExecutionStatus: "NOT_RUN", Evidence: s.evidence(file, pointer),
	})
}

func (s *scan) module(cwd, file string, languages ...string) {
	s.profile.Modules = append(s.profile.Modules, model.Module{Path: cwd, Manifest: file, Languages: languages})
}

func (s *scan) finish() {
	sort.Slice(s.profile.Facts, func(i, j int) bool {
		a, b := s.profile.Facts[i], s.profile.Facts[j]
		return a.Kind+"\x00"+a.Name+"\x00"+a.Evidence[0].Path+"\x00"+a.Evidence[0].Pointer < b.Kind+"\x00"+b.Name+"\x00"+b.Evidence[0].Path+"\x00"+b.Evidence[0].Pointer
	})
	sort.Slice(s.profile.Commands, func(i, j int) bool {
		a, b := s.profile.Commands[i], s.profile.Commands[j]
		return a.Cwd+"\x00"+a.Name+"\x00"+strings.Join(a.Argv, "\x00") < b.Cwd+"\x00"+b.Name+"\x00"+strings.Join(b.Argv, "\x00")
	})
	sort.Slice(s.profile.Modules, func(i, j int) bool {
		a, b := s.profile.Modules[i], s.profile.Modules[j]
		return a.Path+"\x00"+a.Manifest < b.Path+"\x00"+b.Manifest
	})
	for _, values := range []*[]string{&s.profile.AgentConfigs, &s.profile.UnknownItems, &s.profile.Coverage.Excluded} {
		sort.Strings(*values)
		*values = unique(*values)
	}
	if len(s.profile.Modules) == 0 {
		message := "project technical stack is unknown: no supported manifest evidence"
		for _, fact := range s.profile.Facts {
			if fact.Kind == "language" {
				message = "project module structure is unknown: no supported project manifest"
				break
			}
		}
		s.profile.UnknownItems = append(s.profile.UnknownItems, message)
	}
}

func unique(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := values[:1]
	for _, v := range values[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

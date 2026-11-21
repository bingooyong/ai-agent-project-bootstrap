package scanner

import (
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strings"

	"github.com/bingooyong/oh-my-blueprint/internal/strictio"
)

func (s *scan) packageJSON(name string) error {
	data, err := s.read(name)
	if err != nil {
		return err
	}
	var pkg map[string]any
	if !json.Valid(data) {
		return errors.New("package manifest must be JSON")
	}
	if err := strictio.Decode(data, &pkg); err != nil || pkg == nil {
		return errors.New("invalid package manifest")
	}
	cwd := path.Dir(name)
	languages := []string{"javascript"}
	for _, key := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"} {
		v, exists := pkg[key]
		if !exists {
			continue
		}
		deps, ok := v.(map[string]any)
		if !ok {
			return errors.New("dependencies must be a mapping")
		}
		keys := make([]string, 0, len(deps))
		for dep, version := range deps {
			if _, ok := version.(string); !ok {
				return errors.New("dependency version must be a string")
			}
			keys = append(keys, dep)
		}
		sort.Strings(keys)
		for _, dep := range keys {
			pointer := "/" + key + "/" + strings.ReplaceAll(strings.ReplaceAll(dep, "~", "~0"), "/", "~1")
			s.dependency(dep, name, pointer, "npm")
			if dep == "typescript" {
				languages = append(languages, "typescript")
				s.fact("language", "typescript", "", name, pointer)
			}
		}
	}
	s.fact("language", "javascript", "", name, "/")
	sort.Strings(languages)
	s.module(cwd, name, unique(languages)...)
	manager, ambiguous, err := s.packageManager(cwd, pkg)
	if err != nil {
		return err
	}
	if ambiguous {
		s.profile.UnknownItems = append(s.profile.UnknownItems, name+": package manager declarations conflict or are unsupported")
		return nil
	}
	s.fact("build_tool", manager, "", name, "/")
	if scripts, exists := pkg["scripts"]; exists {
		mapping, ok := scripts.(map[string]any)
		if !ok {
			return errors.New("scripts must be a mapping")
		}
		keys := make([]string, 0, len(mapping))
		for key, value := range mapping {
			if _, ok := value.(string); !ok {
				return errors.New("script declaration must be a string")
			}
			if simpleName.MatchString(key) && !strings.HasPrefix(key, "-") {
				keys = append(keys, key)
			} else {
				s.profile.UnknownItems = append(s.profile.UnknownItems, name+": unsupported script key omitted")
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			s.command(key, []string{manager, "run", key}, cwd, name, "/scripts/"+key, "declared")
		}
	}
	if v, ok := pkg["workspaces"]; ok {
		// Child manifests provide module facts. Workspace declarations only
		// describe topology; they never cause execution or unbounded globbing.
		if _, err := workspacePatterns(v); err != nil {
			return errors.New("invalid workspace declaration")
		}
		s.fact("structure", "workspace", "declared", name, "/workspaces")
	}
	return nil
}

func (s *scan) tsconfig(name string) error {
	data, err := s.read(name)
	if err != nil {
		return err
	}
	if !json.Valid(data) {
		// TypeScript allows JSONC. Do not pretend our strict JSON subset has
		// validated it or infer a language from the filename alone.
		s.profile.UnknownItems = append(s.profile.UnknownItems, name+": JSONC or invalid JSON requires TypeScript configuration review")
		return nil
	}
	var config map[string]any
	if err := strictio.Decode(data, &config); err != nil {
		return err
	}
	if _, ok := config["compilerOptions"].(map[string]any); ok {
		s.fact("language", "typescript", "", name, "/compilerOptions")
		return nil
	}
	for _, key := range []string{"include", "files", "references"} {
		if _, ok := config[key].([]any); ok {
			s.fact("language", "typescript", "", name, "/"+key)
			return nil
		}
	}
	s.profile.UnknownItems = append(s.profile.UnknownItems, name+": no supported TypeScript compiler declaration")
	return nil
}

func (s *scan) packageManager(cwd string, pkg map[string]any) (string, bool, error) {
	manager, explicit, ambiguous, err := s.localPackageManager(cwd, pkg)
	if err != nil || explicit || ambiguous {
		return manager, ambiguous, err
	}
	// A nested module inherits only from an explicit workspace declaration,
	// not from arbitrary parent folder names or an unrelated lockfile.
	for parent := path.Dir(cwd); cwd != "."; parent = path.Dir(parent) {
		manifest := path.Join(parent, "package.json")
		var ancestor map[string]any
		if _, exists := s.files[manifest]; exists {
			data, err := s.read(manifest)
			if err != nil || !json.Valid(data) || strictio.Decode(data, &ancestor) != nil {
				return "", false, errors.New("invalid ancestor package manifest")
			}
		}
		declaredWorkspace := false
		var patterns []string
		if v, ok := ancestor["workspaces"]; ok {
			patterns, err = workspacePatterns(v)
			if err != nil {
				return "", false, err
			}
			declaredWorkspace = true
		}
		pnpmWorkspace := path.Join(parent, "pnpm-workspace.yaml")
		if _, exists := s.files[pnpmWorkspace]; exists {
			data, err := s.read(pnpmWorkspace)
			var workspace map[string]any
			if err != nil || strictio.Decode(data, &workspace) != nil {
				return "", false, errors.New("invalid pnpm workspace")
			}
			more, err := workspacePatterns(workspace["packages"])
			if err != nil {
				return "", false, err
			}
			patterns = append(patterns, more...)
			declaredWorkspace = true
		}
		relative := strings.TrimPrefix(cwd, parent+"/")
		if parent == "." {
			relative = cwd
		}
		if declaredWorkspace {
			for _, pattern := range patterns {
				if matchGlob(pattern, relative) {
					manager, explicit, ambiguous, err := s.localPackageManager(parent, ancestor)
					if err != nil || ambiguous {
						return "", ambiguous, err
					}
					if explicit {
						return manager, false, nil
					}
				}
			}
		}
		if parent == "." {
			break
		}
	}
	return "npm", false, nil
}

func workspacePatterns(value any) ([]string, error) {
	if object, ok := value.(map[string]any); ok {
		value = object["packages"]
	}
	array, ok := value.([]any)
	if !ok {
		return nil, errors.New("workspace packages must be a string array")
	}
	patterns := make([]string, 0, len(array))
	for _, value := range array {
		pattern, ok := value.(string)
		if !ok || pattern == "" || len(pattern) > 256 || strings.Contains(pattern, "..") || strings.Contains(pattern, "\\") || strings.HasPrefix(pattern, "/") || strings.HasPrefix(pattern, "!") {
			return nil, errors.New("unsupported workspace package pattern")
		}
		if _, err := path.Match(strings.ReplaceAll(pattern, "**", "*"), ""); err != nil {
			return nil, errors.New("invalid workspace package pattern")
		}
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

func (s *scan) localPackageManager(cwd string, pkg map[string]any) (string, bool, bool, error) {
	declared := ""
	if value, exists := pkg["packageManager"]; exists {
		text, ok := value.(string)
		if !ok {
			return "", false, false, errors.New("package manager must be a string")
		}
		declared = strings.SplitN(text, "@", 2)[0]
		if declared != "npm" && declared != "pnpm" && declared != "yarn" && declared != "bun" {
			return "", true, true, nil
		}
	}
	locks := map[string]string{"package-lock.json": "npm", "npm-shrinkwrap.json": "npm", "pnpm-lock.yaml": "pnpm", "pnpm-workspace.yaml": "pnpm", "yarn.lock": "yarn", "bun.lock": "bun", "bun.lockb": "bun"}
	managers := map[string]bool{}
	if declared != "" {
		managers[declared] = true
	}
	for lock, manager := range locks {
		filename := path.Join(cwd, lock)
		if _, exists := s.files[filename]; exists {
			// Hash lockfile declarations for drift evidence without interpreting
			// or echoing URLs, resolved credentials, or installation hooks.
			if _, err := s.read(filename); err != nil {
				return "", false, false, err
			}
			managers[manager] = true
		}
	}
	if len(managers) > 1 {
		return "", true, true, nil
	}
	for manager := range managers {
		return manager, true, false, nil
	}
	return "npm", false, false, nil
}

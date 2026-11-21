package scanner

import (
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/bingooyong/oh-my-blueprint/internal/model"
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
	manager, err := s.packageManager(cwd, pkg)
	if err != nil {
		return err
	}
	if manager.ambiguous {
		s.profile.UnknownItems = append(s.profile.UnknownItems, name+": package manager declarations conflict or are unsupported")
		return nil
	}
	if len(manager.evidence) != 0 {
		s.profile.Facts = append(s.profile.Facts, model.Fact{
			Kind: "build_tool", Name: manager.name, Confidence: 1, Status: "declared", Evidence: manager.evidence,
		})
	}
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
			s.profile.Commands = append(s.profile.Commands, model.Command{
				Name: key, Argv: []string{manager.name, "run", key}, Cwd: cwd,
				Status: "declared", ExecutionStatus: "NOT_RUN",
				Evidence: append(s.evidence(name, "/scripts/"+key), manager.evidence...),
			})
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

type nodeManager struct {
	name      string
	evidence  []model.Evidence
	ambiguous bool
}

func (s *scan) packageManager(cwd string, pkg map[string]any) (nodeManager, error) {
	manager, err := s.localPackageManager(cwd, pkg)
	if err != nil || manager.name != "" || manager.ambiguous {
		return manager, err
	}
	// A nested module inherits only from an explicit workspace declaration,
	// not from arbitrary parent folder names or an unrelated lockfile.
	for parent := path.Dir(cwd); cwd != "."; parent = path.Dir(parent) {
		manifest := path.Join(parent, "package.json")
		var ancestor map[string]any
		if _, exists := s.files[manifest]; exists {
			data, err := s.read(manifest)
			if err != nil {
				return nodeManager{}, err
			}
			if !json.Valid(data) || strictio.Decode(data, &ancestor) != nil || ancestor == nil {
				return nodeManager{}, errors.New("invalid ancestor package manifest")
			}
		}
		relative := strings.TrimPrefix(cwd, parent+"/")
		if parent == "." {
			relative = cwd
		}
		var membership []model.Evidence
		if v, ok := ancestor["workspaces"]; ok {
			pointer, err := workspaceMember(v, relative, "/workspaces")
			if err != nil {
				return nodeManager{}, err
			}
			if pointer != "" {
				membership = s.evidence(manifest, pointer)
			}
		}
		pnpmWorkspace := path.Join(parent, "pnpm-workspace.yaml")
		if _, exists := s.files[pnpmWorkspace]; exists {
			data, err := s.read(pnpmWorkspace)
			if err != nil {
				return nodeManager{}, err
			}
			var workspace map[string]any
			if strictio.Decode(data, &workspace) != nil {
				return nodeManager{}, errors.New("invalid pnpm workspace")
			}
			pointer, err := workspaceMember(workspace["packages"], relative, "/packages")
			if err != nil {
				return nodeManager{}, err
			}
			if pointer != "" {
				membership = append(membership, s.evidence(pnpmWorkspace, pointer)...)
			}
		}
		if len(membership) != 0 {
			manager, err := s.localPackageManager(parent, ancestor)
			if err != nil || manager.ambiguous {
				return manager, err
			}
			if manager.name != "" {
				manager.evidence = append(manager.evidence, membership...)
				return manager, nil
			}
		}
		if parent == "." {
			break
		}
	}
	return nodeManager{name: "npm"}, nil
}

func workspaceMember(value any, relative, pointer string) (string, error) {
	patterns, err := workspacePatterns(value)
	if err != nil {
		return "", err
	}
	if _, object := value.(map[string]any); object {
		pointer += "/packages"
	}
	for i, pattern := range patterns {
		if matchGlob(pattern, relative) {
			return pointer + "/" + strconv.Itoa(i), nil
		}
	}
	return "", nil
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

func (s *scan) localPackageManager(cwd string, pkg map[string]any) (nodeManager, error) {
	var result nodeManager
	if value, exists := pkg["packageManager"]; exists {
		text, ok := value.(string)
		if !ok {
			return nodeManager{}, errors.New("package manager must be a string")
		}
		result.name, _, _ = strings.Cut(text, "@")
		switch result.name {
		case "npm", "pnpm", "yarn", "bun":
			result.evidence = s.evidence(path.Join(cwd, "package.json"), "/packageManager")
		default:
			return nodeManager{ambiguous: true}, nil
		}
	}
	files := [...]struct{ name, manager string }{
		{"bun.lock", "bun"}, {"bun.lockb", "bun"}, {"npm-shrinkwrap.json", "npm"}, {"package-lock.json", "npm"},
		{"pnpm-lock.yaml", "pnpm"}, {"pnpm-workspace.yaml", "pnpm"}, {"yarn.lock", "yarn"},
	}
	for _, file := range files {
		filename := path.Join(cwd, file.name)
		if _, exists := s.files[filename]; exists {
			// Hash lockfile declarations for drift evidence without interpreting
			// or echoing URLs, resolved credentials, or installation hooks.
			if _, err := s.read(filename); err != nil {
				return nodeManager{}, err
			}
			result.ambiguous = result.ambiguous || (result.name != "" && result.name != file.manager)
			result.name = file.manager
			result.evidence = append(result.evidence, s.evidence(filename, "")...)
		}
	}
	if result.ambiguous {
		return nodeManager{ambiguous: true}, nil
	}
	return result, nil
}

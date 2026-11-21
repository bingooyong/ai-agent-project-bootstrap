package rules

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
)

//go:embed builtin/*.md
var builtinFiles embed.FS

const customDir = ".ombp/rules/custom"

// Project keeps decoded inputs and the raw digests of the bytes they came from.
type Project struct {
	Rules     []Rule
	Overrides []Override
	Inputs    map[string]string
}

// Builtin reads the single version-controlled embedded rule source.
func Builtin() ([]Rule, error) {
	entries, err := builtinFiles.ReadDir("builtin")
	if err != nil {
		return nil, err
	}
	out := make([]Rule, 0, len(entries))
	for _, entry := range entries {
		data, err := builtinFiles.ReadFile("builtin/" + entry.Name())
		if err != nil {
			return nil, err
		}
		r, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("builtin %s: %w", entry.Name(), err)
		}
		r.Source.Path = "builtin/" + entry.Name()
		out = append(out, r)
	}
	return out, nil
}

// LoadCustom reads only Markdown files directly inside the documented custom
// directory. Absence is normal. Entries cannot supply their own source path.
func LoadCustom(root string) ([]Rule, error) {
	r, err := safeio.Open(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	p := Project{Rules: []Rule{}}
	if err := loadCustom(r, &p); err != nil {
		return nil, err
	}
	return p.Rules, nil
}

// LoadProject loads only the documented directories using anchored, bounded
// reads. Input digests describe the same bytes that passed validation.
func LoadProject(root string) (Project, error) {
	r, err := safeio.Open(root)
	if err != nil {
		return Project{}, err
	}
	defer r.Close()
	project := Project{Rules: []Rule{}, Overrides: []Override{}, Inputs: map[string]string{}}
	if err := loadCustom(r, &project); err != nil {
		return Project{}, err
	}
	if err := loadRuleSources(r, overridesDir, ".yaml", maxOverrideBytes, project.Inputs, func(name string, data []byte) error {
		record, err := parseOverride(data)
		if err != nil {
			return err
		}
		record.Path = name
		project.Overrides = append(project.Overrides, record)
		return nil
	}); err != nil {
		return Project{}, err
	}
	return project, nil
}

func loadCustom(r *safeio.Root, project *Project) error {
	return loadRuleSources(r, customDir, ".md", maxRuleBytes, project.Inputs, func(name string, data []byte) error {
		rule, err := Parse(data)
		if err != nil {
			return err
		}
		rule.Source.Path = name
		project.Rules = append(project.Rules, rule)
		return nil
	})
}

func loadRuleSources(r *safeio.Root, directory, suffix string, fileLimit int64, inputs map[string]string, parse func(string, []byte) error) error {
	entries, err := r.ReadDirLimit(directory, maxRules)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("rule directory %s: %w", directory, err)
	}
	var total int
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		if strings.Contains(entry.Name(), "/") || entry.Type()&fs.ModeType != 0 {
			return errors.New("rule source is not a regular file")
		}
		name := path.Join(directory, entry.Name())
		data, err := r.Read(name, fileLimit)
		if err != nil {
			return fmt.Errorf("cannot safely read rule source in %s", directory)
		}
		total += len(data)
		if total > 4<<20 {
			return errors.New("rule source total byte budget exceeded")
		}
		if err := parse(name, data); err != nil {
			return fmt.Errorf("rule source %q: %w", name, err)
		}
		if inputs != nil {
			inputs[name] = safeio.Digest(data)
		}
	}
	return nil
}

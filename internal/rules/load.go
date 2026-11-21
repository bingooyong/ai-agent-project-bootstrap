package rules

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
)

//go:embed builtin/*.md
var builtinFiles embed.FS

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
	const directory = ".ombp/rules/custom"
	entries, err := r.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return []Rule{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("custom rule directory: %w", err)
	}
	if len(entries) > maxRules {
		return nil, errors.New("custom rule directory entry budget exceeded")
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	out := make([]Rule, 0, len(entries))
	var total int
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := path.Join(directory, entry.Name())
		if entry.Type()&fs.ModeSymlink != 0 || entry.IsDir() {
			return nil, fmt.Errorf("custom rule %s is not a regular rule file", name)
		}
		data, err := r.Read(name, maxRuleBytes)
		if err != nil {
			return nil, fmt.Errorf("custom rule %s: %w", name, err)
		}
		total += len(data)
		if total > 4<<20 {
			return nil, errors.New("custom rule total byte budget exceeded")
		}
		rule, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("custom rule %s: %w", name, err)
		}
		rule.Source.Path = name
		out = append(out, rule)
	}
	return out, nil
}

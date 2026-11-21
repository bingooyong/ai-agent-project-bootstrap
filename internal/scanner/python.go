package scanner

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var requirementName = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*)(.*)$`)
var tableName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// pyProject deliberately handles only explicit PEP 621/build-system and Poetry
// declarations. It is not a generic TOML parser or dependency resolver. Dynamic
// and unsupported multiline constructs are reported for review, never guessed.
func (s *scan) pyProject(name string) error {
	data, err := s.read(name)
	if err != nil || !utf8.Valid(data) {
		return errors.New("invalid pyproject")
	}
	lines := bufio.NewScanner(bytes.NewReader(data))
	table, lineNo, hasProject := "", 0, false
	seen := map[string]bool{}
	seenTables := map[string]bool{}
	for lines.Scan() {
		lineNo++
		line := stripComment(strings.TrimSpace(lines.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") || strings.HasPrefix(line, "[[") {
				s.profile.UnknownItems = append(s.profile.UnknownItems, name+": unsupported TOML table construct")
				table = ""
				continue
			}
			table = strings.TrimSpace(line[1 : len(line)-1])
			if !tableName.MatchString(table) {
				return errors.New("invalid TOML table")
			}
			if seenTables[table] {
				return errors.New("duplicate TOML table")
			}
			seenTables[table] = true
			if table == "tool.pytest.ini_options" {
				s.fact("test_tool", "pytest", "python", name, table)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			// Unsupported syntax may belong to an unrelated table; preserve an
			// unknown rather than silently confirming complete TOML validation.
			s.profile.UnknownItems = append(s.profile.UnknownItems, name+": TOML syntax outside supported static subset requires review")
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !tableName.MatchString(key) {
			s.profile.UnknownItems = append(s.profile.UnknownItems, name+": unsupported TOML key requires review")
			continue
		}
		identity := table + "." + key
		if seen[identity] {
			return errors.New("duplicate TOML key")
		}
		seen[identity] = true
		pointer := fmt.Sprintf("%s:line:%d", identity, lineNo)
		if (table == "project" || table == "tool.poetry") && key == "name" {
			if _, err := tomlString(value); err != nil {
				return err
			}
			hasProject = true
		}
		isRequirements := table == "project" && key == "dependencies" || table == "build-system" && key == "requires" || table == "project.optional-dependencies"
		if isRequirements {
			for !arrayClosed(value) {
				if !strings.HasPrefix(value, "[") || !lines.Scan() {
					return errors.New("invalid TOML dependency array")
				}
				lineNo++
				value += " " + stripComment(strings.TrimSpace(lines.Text()))
			}
			deps, err := tomlStrings(value)
			if err != nil {
				return err
			}
			hasProject = true
			for _, dep := range deps {
				packageName, ok := pythonRequirement(dep)
				if !ok {
					return errors.New("invalid Python requirement")
				}
				s.dependency(packageName, name, pointer, "python")
			}
		}
		if table == "tool.poetry.dependencies" || strings.HasPrefix(table, "tool.poetry.group.") && strings.HasSuffix(table, ".dependencies") || table == "tool.poetry.dev-dependencies" {
			if strings.HasPrefix(value, "{") {
				s.profile.UnknownItems = append(s.profile.UnknownItems, name+": inline Poetry dependency tables require review")
				continue
			}
			if _, err := tomlString(value); err != nil {
				return err
			}
			if key != "python" {
				s.dependency(key, name, pointer, "python")
			}
			hasProject = true
		}
		if table == "project" && key == "dynamic" {
			s.profile.UnknownItems = append(s.profile.UnknownItems, name+": dynamic project fields are not evaluated")
		}
	}
	if lines.Err() != nil {
		return errors.New("pyproject line budget exceeded")
	}
	if !hasProject {
		s.profile.UnknownItems = append(s.profile.UnknownItems, name+": no supported explicit Python project declaration")
		return nil
	}
	s.module(path.Dir(name), name, "python")
	s.fact("language", "python", "", name, "project/build-system")
	return nil
}

func (s *scan) requirements(name string) error {
	data, err := s.read(name)
	if err != nil || !utf8.Valid(data) {
		return errors.New("invalid requirements")
	}
	lines, lineNo, found := bufio.NewScanner(bytes.NewReader(data)), 0, false
	for lines.Scan() {
		lineNo++
		line := stripComment(strings.TrimSpace(lines.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "-") || strings.Contains(line, "://") {
			s.profile.UnknownItems = append(s.profile.UnknownItems, name+": indirect or URL requirement not followed")
			continue
		}
		dep, ok := pythonRequirement(line)
		if !ok {
			return errors.New("invalid requirement declaration")
		}
		found = true
		s.dependency(dep, name, fmt.Sprintf("line:%d", lineNo), "python")
	}
	if lines.Err() != nil {
		return errors.New("requirements line budget exceeded")
	}
	if found {
		s.module(path.Dir(name), name, "python")
		s.fact("language", "python", "", name, "requirements")
	}
	return nil
}

func pythonRequirement(line string) (string, bool) {
	m := requirementName.FindStringSubmatch(strings.TrimSpace(line))
	if len(m) == 0 {
		return "", false
	}
	tail := strings.TrimSpace(m[2])
	if tail != "" && !strings.ContainsAny(tail[:1], "[<>=!~;@(") {
		return "", false
	}
	return m[1], true
}

func stripComment(line string) string {
	quote, escaped := byte(0), false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && c == '\\' {
			escaped = true
			continue
		}
		if c == quote {
			quote = 0
		} else if quote == 0 && (c == '\'' || c == '"') {
			quote = c
		} else if quote == 0 && c == '#' {
			return strings.TrimSpace(line[:i])
		}
	}
	return strings.TrimSpace(line)
}

func tomlString(value string) (string, error) {
	if len(value) < 2 {
		return "", errors.New("expected TOML string")
	}
	if value[0] == '\'' && value[len(value)-1] == '\'' && !strings.Contains(value[1:len(value)-1], "'") {
		return value[1 : len(value)-1], nil
	}
	if value[0] != '"' || strings.HasPrefix(value, `"""`) {
		return "", errors.New("unsupported TOML string")
	}
	return strconv.Unquote(value)
}

func arrayClosed(value string) bool {
	return strings.HasPrefix(value, "[") && strings.HasSuffix(strings.TrimSpace(value), "]")
}

func tomlStrings(value string) ([]string, error) {
	if !arrayClosed(value) {
		return nil, errors.New("expected TOML array")
	}
	inside := strings.TrimSpace(value[1 : len(value)-1])
	if inside == "" {
		return []string{}, nil
	}
	var result []string
	for inside != "" {
		quote := inside[0]
		if quote != '"' && quote != '\'' {
			return nil, errors.New("expected TOML string array member")
		}
		end, escaped := -1, false
		for i := 1; i < len(inside); i++ {
			if escaped {
				escaped = false
				continue
			}
			if quote == '"' && inside[i] == '\\' {
				escaped = true
				continue
			}
			if inside[i] == quote {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, errors.New("unterminated TOML array string")
		}
		decoded, err := tomlString(inside[:end+1])
		if err != nil {
			return nil, err
		}
		result = append(result, decoded)
		inside = strings.TrimSpace(inside[end+1:])
		if inside == "" {
			break
		}
		if inside[0] != ',' {
			return nil, errors.New("TOML array comma required")
		}
		inside = strings.TrimSpace(inside[1:])
	}
	return result, nil
}

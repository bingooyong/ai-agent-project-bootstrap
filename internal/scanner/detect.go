package scanner

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/bingooyong/oh-my-blueprint/internal/strictio"
)

var simpleName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,100}$`)
var makeTarget = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_. -]*):([^=]|$)`)
var moduleName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~/-]*$`)
var goVersion = regexp.MustCompile(`^([1-9][0-9]*)\.(0|[1-9][0-9]*)(\.(0|[1-9][0-9]*))?([a-z]+[0-9]+)?$`)
var goToolchain = regexp.MustCompile(`^default$|^go1($|\.)`)

func (s *scan) detect() error {
	names := make([]string, 0, len(s.files))
	for name := range s.files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		base := path.Base(name)
		if agentConfig(name) {
			s.profile.AgentConfigs = append(s.profile.AgentConfigs, name)
			continue
		}
		var err error
		switch {
		case base == "go.mod":
			err = s.goMod(name)
		case base == "package.json":
			err = s.packageJSON(name)
		case base == "tsconfig.json":
			err = s.tsconfig(name)
		case base == "pyproject.toml":
			err = s.pyProject(name)
		case base == "requirements.txt" || (strings.HasPrefix(base, "requirements-") && strings.HasSuffix(base, ".txt")):
			err = s.requirements(name)
		case base == "Makefile" || base == "makefile" || base == "GNUmakefile":
			err = s.makefile(name)
		case strings.HasPrefix(name, ".github/workflows/") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")):
			err = s.declaration(name, "ci", "github-actions")
		case base == ".gitlab-ci.yml":
			err = s.declaration(name, "ci", "gitlab-ci")
		case base == "Jenkinsfile":
			err = s.declaration(name, "ci", "jenkins")
		case base == "Dockerfile":
			err = s.declaration(name, "deployment", "docker")
		case base == "README" || base == "README.md" || base == "README.rst" || base == "README.txt":
			err = s.declaration(name, "documentation", "readme")
		case base == "docker-compose.yml" || base == "docker-compose.yaml" || base == "compose.yml" || base == "compose.yaml":
			err = s.declaration(name, "deployment", "docker-compose")
		}
		if err != nil {
			// Parser diagnostics can contain untrusted declaration values. Only
			// return our static path/message rather than the underlying payload.
			return fmt.Errorf("cannot parse supported declaration %q", name)
		}
	}
	return nil
}

func agentConfig(name string) bool {
	base := path.Base(name)
	if base == "AGENTS.md" || base == "AGENTS.override.md" || base == "CLAUDE.md" || base == "GEMINI.md" {
		return true
	}
	return strings.Contains("/"+name, "/.cursor/rules/") && strings.HasSuffix(name, ".mdc") ||
		strings.Contains("/"+name, "/.claude/rules/") && strings.HasSuffix(name, ".md") ||
		strings.Contains("/"+name, "/.agents/skills/") && base == "SKILL.md"
}

func (s *scan) declaration(name, kind, value string) error {
	data, err := s.read(name)
	if err != nil {
		return err
	}
	if !utf8.Valid(data) || len(bytes.TrimSpace(data)) == 0 {
		return errors.New("empty or non-UTF8 declaration")
	}
	if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
		var value map[string]any
		if err := strictio.Decode(data, &value); err != nil || len(value) == 0 {
			return errors.New("invalid YAML declaration")
		}
	}
	s.fact(kind, value, "", name, "")
	return nil
}

func (s *scan) goMod(name string) error {
	data, err := s.read(name)
	if err != nil || !utf8.Valid(data) {
		return errors.New("invalid Go module")
	}
	lines := bufio.NewScanner(bytes.NewReader(data))
	hasModule, hasGoVersion, hasToolchain, requireBlock, otherBlock := false, false, false, false, false
	lineNo := 0
	for lines.Scan() {
		lineNo++
		line := strings.TrimSpace(strings.SplitN(lines.Text(), "//", 2)[0])
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "module":
			if requireBlock || otherBlock || hasModule || len(fields) != 2 || !moduleName.MatchString(fields[1]) {
				return errors.New("invalid module directive")
			}
			hasModule = true
		case "go":
			if requireBlock || otherBlock || hasGoVersion || len(fields) != 2 || !goVersion.MatchString(fields[1]) {
				return errors.New("invalid go directive")
			}
			hasGoVersion = true
			s.fact("runtime", "go", fields[1], name, fmt.Sprintf("line:%d", lineNo))
		case "toolchain":
			if requireBlock || otherBlock || hasToolchain || len(fields) != 2 || !goToolchain.MatchString(fields[1]) {
				return errors.New("invalid toolchain directive")
			}
			hasToolchain = true
			s.profile.UnknownItems = append(s.profile.UnknownItems, name+": toolchain version is declared but not validated against installed runtime")
		case "require":
			if len(fields) == 2 && fields[1] == "(" {
				requireBlock = true
			} else if len(fields) == 3 && moduleName.MatchString(fields[1]) {
				s.dependency(fields[1], name, fmt.Sprintf("line:%d", lineNo), "go")
			} else {
				return errors.New("invalid require directive")
			}
		case ")":
			if !requireBlock && !otherBlock || len(fields) != 1 {
				return errors.New("unmatched module block")
			}
			requireBlock = false
			otherBlock = false
		case "godebug", "replace", "exclude", "retract", "tool", "ignore":
			if len(fields) < 2 || requireBlock || otherBlock {
				return errors.New("invalid module directive")
			}
			if fields[len(fields)-1] == "(" {
				otherBlock = true
			}
			// Go's evolving directive grammar is not a shell interface. These
			// declarations remain unexpanded and are explicit review items.
			s.profile.UnknownItems = append(s.profile.UnknownItems, name+": additional Go module directives are not evaluated")
		default:
			if requireBlock {
				if len(fields) != 2 || !moduleName.MatchString(fields[0]) {
					return errors.New("invalid module requirement")
				}
				s.dependency(fields[0], name, fmt.Sprintf("line:%d", lineNo), "go")
			} else if !otherBlock {
				return errors.New("unsupported module syntax")
			}
		}
	}
	if lines.Err() != nil || !hasModule || requireBlock || otherBlock {
		return errors.New("incomplete Go module")
	}
	cwd := path.Dir(name)
	s.module(cwd, name, "go")
	s.fact("language", "go", "", name, "module")
	s.fact("build_tool", "go", "", name, "module")
	s.command("build", []string{"go", "build", "./..."}, cwd, name, "module", "candidate")
	s.command("test", []string{"go", "test", "./..."}, cwd, name, "module", "candidate")
	return nil
}

func (s *scan) makefile(name string) error {
	data, err := s.read(name)
	if err != nil || !utf8.Valid(data) {
		return errors.New("invalid Makefile")
	}
	lines := bufio.NewScanner(bytes.NewReader(data))
	lineNo := 0
	hasTarget := false
	for lines.Scan() {
		lineNo++
		line := lines.Text()
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		m := makeTarget.FindStringSubmatch(line)
		if len(m) == 0 {
			if strings.Contains(line, "$") && strings.Contains(line, ":") {
				s.profile.UnknownItems = append(s.profile.UnknownItems, name+": dynamic Make target requires review")
			}
			continue
		}
		for _, target := range strings.Fields(m[1]) {
			if strings.HasPrefix(target, ".") || !simpleName.MatchString(target) {
				continue
			}
			hasTarget = true
			s.command(target, []string{"make", target}, path.Dir(name), name, fmt.Sprintf("line:%d", lineNo), "candidate")
		}
	}
	if lines.Err() != nil {
		return errors.New("Makefile line budget exceeded")
	}
	if hasTarget {
		s.fact("build_tool", "make", "", name, "targets")
	}
	return nil
}

func (s *scan) dependency(name, file, pointer, ecosystem string) {
	normal := strings.ToLower(strings.ReplaceAll(name, "_", "-"))
	var kind, detected string
	switch {
	case normal == "react" || normal == "react-dom":
		kind, detected = "framework", "react"
	case normal == "fastapi" || normal == "django" || normal == "flask":
		kind, detected = "framework", normal
	case normal == "github.com/gin-gonic/gin":
		kind, detected = "framework", "gin"
	case strings.HasPrefix(normal, "github.com/labstack/echo/"):
		kind, detected = "framework", "echo"
	case normal == "mysql" || normal == "mysql2" || normal == "mysqlclient" || normal == "pymysql" || normal == "mysql-connector-python" || normal == "github.com/go-sql-driver/mysql":
		kind, detected = "database", "mysql"
	case normal == "pg" || normal == "postgres" || normal == "psycopg" || normal == "psycopg2" || normal == "psycopg2-binary" || normal == "asyncpg" || normal == "github.com/lib/pq" || strings.HasPrefix(normal, "github.com/jackc/pgx/"):
		kind, detected = "database", "postgresql"
	case normal == "oracledb" || normal == "cx-oracle" || normal == "github.com/godror/godror":
		kind, detected = "database", "oracle"
	case normal == "gorm.io/gorm":
		kind, detected = "orm", "gorm"
	case normal == "sqlalchemy" || normal == "prisma" || normal == "@prisma/client":
		kind, detected = "orm", strings.TrimPrefix(strings.TrimSuffix(normal, "/client"), "@")
	case normal == "pytest" || normal == "jest" || normal == "vitest" || normal == "playwright" || normal == "@playwright/test":
		kind, detected = "test_tool", strings.TrimPrefix(strings.TrimSuffix(normal, "/test"), "@")
	}
	if kind != "" {
		s.fact(kind, detected, ecosystem, file, pointer)
	}
}

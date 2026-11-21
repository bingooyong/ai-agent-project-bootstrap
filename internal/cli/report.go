package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bingooyong/oh-my-blueprint/internal/rules"
	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
	"github.com/bingooyong/oh-my-blueprint/internal/updater"
)

func validReportPath(name string) bool {
	if safeio.ValidatePath(name) != nil || name == "." || safeio.Sensitive(name) {
		return false
	}
	parts := strings.Split(strings.ToLower(name), "/")
	for i, part := range parts {
		switch part {
		case ".ombp":
			if i != 0 || len(parts) < 3 || parts[1] != "reports" {
				return false
			}
		case ".git", ".hg", ".svn", ".github", ".trellis", ".agents", ".claude", ".codex", ".cursor", ".gemini":
			return false
		}
	}
	base := parts[len(parts)-1]
	switch base {
	case "agents.md", "agents.override.md", "claude.md", "gemini.md", "skill.md", ".mcp.json",
		".gitlab-ci.yml", ".gitmodules", ".gitattributes", ".gitignore",
		"go.mod", "go.sum", "go.work", "package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb",
		"tsconfig.json", "pyproject.toml", "requirements.txt", "makefile", "gnumakefile", "jenkinsfile", "dockerfile",
		"readme", "readme.md", "readme.rst", "readme.txt", "compose.yml", "compose.yaml", "docker-compose.yml", "docker-compose.yaml":
		return false
	default:
		return !(strings.HasPrefix(base, "requirements-") && strings.HasSuffix(base, ".txt"))
	}
}

func reportTarget(cmd *cobra.Command, args []string) string {
	switch cmd.Name() {
	case "version", "list", "show":
		return "."
	default:
		return target(args)
	}
}

func sameReportPath(a, b string) bool {
	return b != "" && strings.EqualFold(a, b)
}

func samePlanAndReport(projectPath, reportPath, planOut string) bool {
	if planOut == "" {
		return false
	}
	projectPath, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		return false
	}
	reportPath = filepath.Join(projectPath, filepath.FromSlash(reportPath))
	if !strings.EqualFold(filepath.Base(reportPath), filepath.Base(planOut)) {
		return false
	}
	// Compare directories by identity so root aliases and Darwin's system
	// aliases cannot turn distinct input spellings into the same destination.
	reportParent, _, err := safeio.OpenExternalParent(reportPath)
	if err != nil {
		return false
	}
	defer reportParent.Close()
	planParent, _, err := safeio.OpenExternalParent(planOut)
	if err != nil {
		return false
	}
	defer planParent.Close()
	reportDir, err := reportParent.Lstat(".")
	planDir, planErr := planParent.Lstat(".")
	return err == nil && planErr == nil && os.SameFile(reportDir, planDir)
}

func reportWriteError(err error) *commandError {
	if errors.Is(err, safeio.ErrUnsafeOutput) {
		return &commandError{4, "report output crosses a link or an invalid parent"}
	}
	return &commandError{3, "report file already exists or could not be safely created"}
}

func writeReport(w io.Writer, format string, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if format == "json" {
		return enc.Encode(r)
	}
	if _, err := fmt.Fprintf(w, "%s: %s\n", r.Command, r.Result); err != nil {
		return err
	}
	if r.Data != nil {
		if err := enc.Encode(r.Data); err != nil {
			return err
		}
	}
	for _, f := range r.Findings {
		if _, err := fmt.Fprintf(w, "%s: %s\n", f.Code, f.Evidence); err != nil {
			return err
		}
	}
	return nil
}

func saveReport(root *safeio.Root, name, format string, r Report) error {
	switch data := r.Data.(type) {
	case updater.Plan:
		// The reviewed plan can contain machine-local paths and full file
		// contents. Keep those out of the persistent report.
		type change struct {
			Path         string `json:"path"`
			BeforeSHA256 string `json:"before_sha256"`
			AfterSHA256  string `json:"after_sha256"`
		}
		summary := make([]change, 0, len(data.Changes))
		for _, c := range data.Changes {
			summary = append(summary, change{c.Path, c.BeforeSHA256, c.AfterSHA256})
		}
		r.Data = struct {
			Fingerprint string   `json:"plan_fingerprint"`
			Changes     []change `json:"changes"`
		}{data.Fingerprint, summary}
	case updater.RecoveryReview:
		paths := make([]string, 0, len(data.Entries))
		for _, e := range data.Entries {
			paths = append(paths, e.Path)
		}
		r.Data = struct {
			JournalSHA256 string   `json:"journal_sha256,omitempty"`
			MutexSHA256   string   `json:"mutex_sha256,omitempty"`
			State         string   `json:"state"`
			Paths         []string `json:"paths"`
		}{data.JournalSHA256, data.MutexSHA256, data.State, paths}
	case rules.Resolution:
		inventory := make([]updater.RuleLock, 0, len(data.Rules))
		for _, rule := range data.Rules {
			inventory = append(inventory, updater.RuleLock{ID: rule.ID, Version: rule.Version, SHA256: rule.SHA256})
		}
		r.Data = struct {
			Rules            []updater.RuleLock     `json:"rules"`
			Fingerprint      string                 `json:"fingerprint"`
			Warnings         []string               `json:"warnings"`
			SelectionReasons map[string][]string    `json:"selection_reasons"`
			Overrides        []updater.OverrideLock `json:"overrides,omitempty"`
		}{inventory, data.Fingerprint, data.Warnings, data.SelectionReasons, updater.LockedOverrides(data)}
	}
	var buf bytes.Buffer
	if err := writeReport(&buf, format, r); err != nil {
		return err
	}
	return root.Replace(name, buf.Bytes(), "")
}

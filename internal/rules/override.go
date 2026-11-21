package rules

import (
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
	"github.com/bingooyong/oh-my-blueprint/internal/strictio"
)

const (
	overridesDir     = ".ombp/rules/overrides"
	maxOverrideBytes = 16 << 10
)

type RuleVersion struct {
	Version string `json:"version" yaml:"version"`
	SHA256  string `json:"sha256" yaml:"sha256"`
}

// Override explicitly binds two exact custom rule definitions and their scope.
// The record does not confer authority to alter protected policy.
type Override struct {
	SchemaVersion string      `json:"schema_version" yaml:"schema_version"`
	RuleID        string      `json:"rule_id" yaml:"rule_id"`
	Before        RuleVersion `json:"before" yaml:"before"`
	After         RuleVersion `json:"after" yaml:"after"`
	Scope         Scope       `json:"scope" yaml:"scope"`
	Reason        string      `json:"reason" yaml:"reason"`
	Owner         string      `json:"owner" yaml:"owner"`
	Path          string      `json:"path,omitempty" yaml:"-"`
}

func parseOverride(data []byte) (Override, error) {
	var record Override
	if len(data) > maxOverrideBytes || strictio.Decode(data, &record) != nil {
		return Override{}, errors.New("override violates strict schema")
	}
	return normalizeOverride(record)
}

func normalizeOverride(record Override) (Override, error) {
	if record.SchemaVersion != "1.0" || !validID(record.RuleID) ||
		!validRuleVersion(record.Before) || !validRuleVersion(record.After) || record.Before.Version == record.After.Version {
		return Override{}, errors.New("override requires an exact rule ID and distinct versions with SHA-256 digests")
	}
	if strings.TrimSpace(record.Reason) == "" || len(record.Reason) > 1024 || hasControl(record.Reason) ||
		strings.TrimSpace(record.Owner) == "" || len(record.Owner) > 128 || hasControl(record.Owner) {
		return Override{}, errors.New("override requires a bounded nonempty owner and reason")
	}
	if record.Path != "" && (safeio.ValidatePath(record.Path) != nil || safeio.Sensitive(record.Path) ||
		path.Dir(record.Path) != overridesDir || !strings.HasSuffix(record.Path, ".yaml")) {
		return Override{}, errors.New("invalid override source path")
	}
	scope, err := normalizeScope(record.Scope)
	if err != nil {
		return Override{}, fmt.Errorf("invalid override scope: %w", err)
	}
	record.Scope = scope
	return record, nil
}

func validRuleVersion(ref RuleVersion) bool {
	if !versionPattern.MatchString(ref.Version) || len(ref.SHA256) != 64 || strings.ToLower(ref.SHA256) != ref.SHA256 {
		return false
	}
	_, err := hex.DecodeString(ref.SHA256)
	return err == nil
}

func applyOverride(record Override, versions map[string]Rule) (Rule, error) {
	before, haveBefore := versions[record.Before.Version]
	after, haveAfter := versions[record.After.Version]
	if len(versions) != 2 || !haveBefore || !haveAfter || before.SHA256 != record.Before.SHA256 || after.SHA256 != record.After.SHA256 {
		return Rule{}, fmt.Errorf("override for rule %s does not match exactly two registered definitions", record.RuleID)
	}
	if !before.Overridable || !localOverrideSeverity(before.Severity) || !localOverrideSeverity(after.Severity) {
		return Rule{}, fmt.Errorf("override for rule %s is forbidden by policy", record.RuleID)
	}
	if !sameScope(before.Scope, record.Scope) || !sameScope(after.Scope, record.Scope) ||
		string(jsonBytes(before.When)) != string(jsonBytes(after.When)) {
		return Rule{}, fmt.Errorf("override for rule %s changes scope or applicability", record.RuleID)
	}
	return after, nil
}

func localOverrideSeverity(severity string) bool { return severity == "P2" || severity == "P3" }

func sameScope(a, b Scope) bool {
	return slices.Equal(a.Modules, b.Modules) && slices.Equal(a.Paths, b.Paths)
}

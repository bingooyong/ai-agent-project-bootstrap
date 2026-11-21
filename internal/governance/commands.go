package governance

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/bingooyong/oh-my-blueprint/internal/model"
	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
)

var commandLabel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,100}$`)
var commandLine = regexp.MustCompile(`^line:[1-9][0-9]{0,7}$`)

// commandSourceFindings compares declarations, never command execution. Evidence
// hashes cover whole source files and are handled by PROJECT_SOURCE_DRIFT.
// Excluded or unsupported declarations cannot prove removal.
func commandSourceFindings(old, current model.Profile) []Finding {
	type identity struct{ cwd, name string }
	type declaration struct {
		identity
		path, pointer string
	}
	type invocation struct {
		identity
		argv string
	}
	byDeclaration := make(map[declaration]model.Command)
	byInvocation := make(map[invocation]bool)
	for _, command := range current.Commands {
		id := identity{command.Cwd, command.Name}
		byInvocation[invocation{id, strings.Join(command.Argv, "\x00")}] = true
		for _, source := range command.Evidence {
			byDeclaration[declaration{id, source.Path, source.Pointer}] = command
		}
	}
	excluded := make(map[string]bool, len(current.Coverage.Excluded))
	for _, name := range current.Coverage.Excluded {
		excluded[name] = true
	}
	unknownSources := make(map[string]bool)
	for _, unknown := range current.UnknownItems {
		// Scanner diagnostics for an individual source are prefixed with its
		// safe relative path. General unknowns are not tied to a source.
		if name, _, ok := strings.Cut(unknown, ":"); ok {
			unknownSources[name] = true
		}
	}
	findings := []Finding{}
	for _, command := range old.Commands {
		id := identity{command.Cwd, command.Name}
		location := Location{Path: ".ombp/profile.json", Pointer: "commands"}
		code := "COMMAND_SOURCE_UNVERIFIED"
		evidence := "A locked command has no usable source location in its stored profile"
		if validCommandSource(command, old.Inputs) {
			source := command.Evidence[0]
			location = Location{Path: source.Path, Pointer: source.Pointer}
			candidate, found := byDeclaration[declaration{id, source.Path, source.Pointer}]
			switch {
			case found:
				if slices.Equal(command.Argv, candidate.Argv) && command.Status == candidate.Status {
					continue
				}
				code = "COMMAND_SOURCE_DRIFT"
				evidence = "The declaration remains at this location and its static invocation or status changed"
			case current.Coverage.Status != "complete" || unknownSources[source.Path] || sourceExcluded(source.Path, excluded):
				evidence = "The command source was excluded or could not be fully interpreted"
			case byInvocation[invocation{id, strings.Join(command.Argv, "\x00")}]:
				code = "COMMAND_SOURCE_DRIFT"
				evidence = "The same invocation was found at a different source location"
			default:
				code = "COMMAND_SOURCE_REMOVED"
				evidence = "The previously recorded declaration is absent from the current static command inventory"
			}
		}
		if location.Path != ".ombp/profile.json" {
			evidence = fmt.Sprintf("Command %q. %s", command.Name, evidence)
		}
		findings = append(findings, Finding{
			Code: code, Severity: "Medium", RuleIDs: []string{"commands.source-traceable"},
			Location: location, Evidence: evidence,
			Impact:               "Agent instructions may refer to stale or unverified build and test declarations",
			Recommendation:       "Review the source and scan coverage before generating an explicit update",
			RequiresConfirmation: true, Method: "static",
		})
	}
	return findings
}

func sourceExcluded(name string, excluded map[string]bool) bool {
	for ; name != "."; name = path.Dir(name) {
		if excluded[name] {
			return true
		}
	}
	return false
}

func validCommandSource(command model.Command, inputs map[string]string) bool {
	if len(command.Evidence) == 0 || !commandLabel.MatchString(command.Name) {
		return false
	}
	source := command.Evidence[0]
	if len(source.Path) > 4096 || safeio.ValidatePath(source.Path) != nil || safeio.Sensitive(source.Path) ||
		path.Dir(source.Path) != command.Cwd || source.SHA256 == "" || inputs[source.Path] != source.SHA256 {
		return false
	}
	return source.Pointer == "module" || source.Pointer == "/scripts/"+command.Name || commandLine.MatchString(source.Pointer)
}

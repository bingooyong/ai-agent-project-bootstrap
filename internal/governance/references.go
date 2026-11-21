package governance

import (
	"errors"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
)

const (
	maxReferenceBytes = 8 << 20
	maxReferenceLinks = 512
	maxReferencePath  = 1024
	documentationRule = "core.documentation-consistency"
)

// The supported subset is a single-line inline text link with a simple label
// and a bare or angle-bracket destination. Titles, reference-style links, raw
// HTML links, images and multiline links are outside this check.
var inlineReference = regexp.MustCompile("^\\[[^\\[\\]\\r\\n]+\\]\\((<[^<>\\r\\n]+>|[^\\s<>()]+)\\)")

type referenceChecker struct {
	root      *safeio.Root
	seen      map[string]bool
	bytes     int
	links     int
	exhausted bool
}

func newReferenceChecker(root *safeio.Root) *referenceChecker {
	return &referenceChecker{root: root, seen: map[string]bool{}}
}

// check inspects already-read instruction bytes once. The caller checks
// documents in path order when a common budget can limit coverage.
func (c *referenceChecker) check(file string, data []byte) (findings []Finding, incomplete bool) {
	if c.exhausted || c.seen[file] {
		return nil, false
	}
	c.seen[file] = true
	c.bytes += len(data)
	if c.bytes > maxReferenceBytes {
		c.exhausted = true
		return []Finding{referenceFinding(file, 0, "DOCUMENT_REFERENCE_BUDGET_EXCEEDED", "Instruction reference inspection reached its byte budget", false)}, true
	}
	if !utf8.Valid(data) {
		return []Finding{referenceFinding(file, 0, "DOCUMENT_REFERENCE_UNVERIFIED", "Instructions are outside the supported UTF-8 text format", false)}, true
	}

	var markup referenceMarkup
	lineNumber := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		lineNumber++
		for target := range markup.targets(strings.TrimSuffix(line, "\r")) {
			c.links++
			if c.links > maxReferenceLinks {
				c.exhausted = true
				return append(findings, referenceFinding(file, lineNumber, "DOCUMENT_REFERENCE_BUDGET_EXCEEDED", "Instruction reference inspection reached its link budget", false)), true
			}
			name, err := localReferencePath(file, target)
			if err == nil && name != "" {
				err = c.root.CheckReference(name)
			}
			switch {
			case err == nil:
			case errors.Is(err, fs.ErrNotExist):
				findings = append(findings, referenceFinding(file, lineNumber, "DOCUMENT_REFERENCE_MISSING", "A local Markdown link target is missing", false))
			case errors.Is(err, safeio.ErrUnsafeReference):
				findings = append(findings, referenceFinding(file, lineNumber, "DOCUMENT_REFERENCE_UNSAFE", "A Markdown link target violates the local path or file-type policy", true))
			default:
				findings = append(findings, referenceFinding(file, lineNumber, "DOCUMENT_REFERENCE_UNVERIFIED", "A local Markdown link target could not be inspected", false))
				incomplete = true
			}
		}
	}
	return findings, incomplete
}

func referenceFinding(file string, line int, code, evidence string, blocking bool) Finding {
	location := Location{Path: file, Line: line}
	severity := "Medium"
	if blocking {
		severity = "High"
	}
	return Finding{
		Code: code, Severity: severity, Blocking: blocking, RuleIDs: []string{documentationRule},
		Location: location, Evidence: evidence, Method: "static", RequiresConfirmation: true,
		Impact:         "The instruction link cannot be confirmed as usable local navigation",
		Recommendation: "Review the source line and use a valid link within the project",
	}
}

// localReferencePath returns an empty path for links outside the scope of this
// check. The raw destination is never copied to the report.
func localReferencePath(file, target string) (string, error) {
	if strings.HasPrefix(target, "<") {
		target = target[1 : len(target)-1]
	}
	raw := target
	if end := strings.IndexAny(raw, "?#"); end >= 0 {
		raw = raw[:end]
	}
	if raw == "" {
		return "", nil
	}
	// A leading drive letter is a local filesystem path, not a network scheme.
	if len(raw) >= 2 && raw[1] == ':' {
		return "", safeio.ErrUnsafeReference
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", safeio.ErrUnsafeReference
	}
	if strings.EqualFold(parsed.Scheme, "file") {
		return "", safeio.ErrUnsafeReference
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		return "", nil
	}
	name := parsed.Path
	if len(name) > maxReferencePath || !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 ||
		path.IsAbs(name) || strings.ContainsAny(name, "\\:") || safeio.Sensitive(name) {
		return "", safeio.ErrUnsafeReference
	}
	name = path.Join(path.Dir(file), name)
	if safeio.ValidatePath(name) != nil {
		return "", safeio.ErrUnsafeReference
	}
	return name, nil
}

type referenceMarkup struct {
	fence   byte
	width   int
	comment bool
}

// targets skips code spans, fenced blocks, indented examples and HTML comments
// before recognizing the supported inline links.
func (m *referenceMarkup) targets(line string) func(func(string) bool) {
	return func(yield func(string) bool) {
		trimmed := strings.TrimLeft(line, " ")
		if !m.comment && len(line)-len(trimmed) <= 3 && len(trimmed) >= 3 {
			char := trimmed[0]
			if char == '`' || char == '~' {
				run := markerRun(trimmed, char)
				if m.fence == 0 && run >= 3 {
					m.fence, m.width = char, run
					return
				}
				if char == m.fence && run >= m.width && strings.TrimSpace(trimmed[run:]) == "" {
					m.fence, m.width = 0, 0
					return
				}
			}
		}
		if m.fence != 0 || !m.comment && (len(line)-len(trimmed) >= 4 || strings.HasPrefix(trimmed, "\t")) {
			return
		}
		for len(line) > 0 {
			switch {
			case m.comment:
				end := strings.Index(line, "-->")
				if end < 0 {
					return
				}
				m.comment = false
				line = line[end+3:]
			case strings.HasPrefix(line, "<!--"):
				m.comment = true
				line = line[4:]
			case line[0] == '\\' && len(line) > 1:
				line = line[2:]
			case line[0] == '`':
				width := markerRun(line, '`')
				line = afterCodeSpan(line[width:], width)
			case line[0] == '[' || strings.HasPrefix(line, "!["):
				image := line[0] == '!'
				if image {
					line = line[1:]
				}
				index := inlineReference.FindStringSubmatchIndex(line)
				if index == nil {
					line = line[1:]
					continue
				}
				if !image && !yield(line[index[2]:index[3]]) {
					return
				}
				line = line[index[1]:]
			default:
				line = line[1:]
			}
		}
	}
}

func markerRun(s string, marker byte) int {
	n := 0
	for n < len(s) && s[n] == marker {
		n++
	}
	return n
}

func afterCodeSpan(line string, width int) string {
	for {
		i := strings.IndexByte(line, '`')
		if i < 0 {
			return ""
		}
		n := markerRun(line[i:], '`')
		line = line[i+n:]
		if n == width {
			return line
		}
	}
}

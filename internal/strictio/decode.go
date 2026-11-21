// Package strictio decodes bounded data, never executable configuration.
package strictio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const maxDepth = 48
const maxNodes = 100000

// Decode accepts one YAML or JSON document with exact, known typed fields.
func Decode(data []byte, out any) error {
	if len(data) > 4<<20 || !utf8.Valid(data) {
		return errors.New("document exceeds size limit or is not UTF-8")
	}
	var tree yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(&tree); err != nil {
		return fmt.Errorf("invalid document: %w", err)
	}
	var extra yaml.Node
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("exactly one document required")
	}
	count := 0
	if err := validate(&tree, 0, &count); err != nil {
		return err
	}
	t := reflect.TypeOf(out)
	if t == nil || t.Kind() != reflect.Pointer || reflect.ValueOf(out).IsNil() {
		return errors.New("decoder target must be a nonnil pointer")
	}
	if len(tree.Content) != 1 {
		return errors.New("document must contain a value")
	}
	if err := checkType(tree.Content[0], t.Elem()); err != nil {
		return err
	}
	d = yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(out); err != nil {
		return fmt.Errorf("invalid typed document: %w", err)
	}
	return nil
}

var decimalInteger = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// yaml's compatibility coercions (e.g. numbers into strings) are not part of
// our protocol. Validate scalar types before its typed decoder is invoked.
func checkType(n *yaml.Node, t reflect.Type) error {
	if t.Kind() == reflect.Pointer {
		return checkType(n, t.Elem())
	}
	bad := errors.New("document value does not match declared type")
	switch t.Kind() {
	case reflect.Interface:
		return nil
	case reflect.String:
		if n.Tag != "!!str" {
			return bad
		}
	case reflect.Bool:
		if n.Tag != "!!bool" || (n.Value != "true" && n.Value != "false") {
			return bad
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n.Tag != "!!int" || !decimalInteger.MatchString(n.Value) {
			return bad
		}
	case reflect.Float32, reflect.Float64:
		if n.Tag != "!!float" && n.Tag != "!!int" {
			return bad
		}
	case reflect.Struct:
		if n.Kind != yaml.MappingNode {
			return bad
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = strings.ToLower(f.Name)
			}
			fields[name] = f.Type
		}
		for i := 0; i < len(n.Content); i += 2 {
			field, ok := fields[n.Content[i].Value]
			if !ok {
				return errors.New("unknown document field")
			}
			if err := checkType(n.Content[i+1], field); err != nil {
				return err
			}
		}
	case reflect.Map:
		if n.Kind != yaml.MappingNode || t.Key().Kind() != reflect.String {
			return bad
		}
		for i := 1; i < len(n.Content); i += 2 {
			if err := checkType(n.Content[i], t.Elem()); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if n.Kind != yaml.SequenceNode {
			return bad
		}
		for _, child := range n.Content {
			if err := checkType(child, t.Elem()); err != nil {
				return err
			}
		}
	default:
		return errors.New("unsupported document target type")
	}
	return nil
}

func validate(n *yaml.Node, depth int, count *int) error {
	*count++
	if depth > maxDepth || *count > maxNodes {
		return errors.New("document depth or node budget exceeded")
	}
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return errors.New("YAML anchors and aliases are not supported")
	}
	if n.Tag == "!!null" || (n.Tag != "" && n.Tag != "!!map" && n.Tag != "!!seq" && n.Tag != "!!str" && n.Tag != "!!bool" && n.Tag != "!!int" && n.Tag != "!!float") {
		return errors.New("null or unsupported YAML type")
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || seen[key.Value] {
				return errors.New("duplicate or non-string mapping key")
			}
			seen[key.Value] = true
		}
	}
	for _, child := range n.Content {
		if err := validate(child, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}

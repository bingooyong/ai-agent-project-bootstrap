//go:build ignore

// Generate the data-shape contracts from the same tagged types used by OMBP.
// Business validation (hashes, dependencies, ownership, budgets) remains in Go.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/bingooyong/oh-my-blueprint/internal/adapters"
	"github.com/bingooyong/oh-my-blueprint/internal/cli"
	"github.com/bingooyong/oh-my-blueprint/internal/model"
	"github.com/bingooyong/oh-my-blueprint/internal/rules"
	"github.com/bingooyong/oh-my-blueprint/internal/updater"
)

type object = map[string]any

func main() {
	check := flag.Bool("check", false, "check committed schemas without writing")
	flag.Parse()
	types := map[string]any{
		"project-profile": model.Profile{}, "project-config": updater.Config{}, "rule": rules.Rule{},
		"adapter": adapters.Capability{}, "project-lock": updater.Lock{}, "plan": updater.Plan{},
		"cli-report": cli.Report{}, "rule-override": rules.Override{},
	}
	names := make([]string, 0, len(types))
	for name := range types {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		schema := shape(reflect.TypeOf(types[name]), name == "rule" || name == "rule-override")
		schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
		schema["title"] = "OMBP " + name + " data shape"
		schema["description"] = "Generated from Go types. This schema describes shape only; OMBP also enforces business, source, ownership and safety invariants."
		if name == "cli-report" {
			applyReportProtocol(schema)
		}
		data, err := json.MarshalIndent(schema, "", "  ")
		must(err)
		data = append(data, '\n')
		filename := filepath.Join("schemas", name+".schema.json")
		if *check {
			old, err := os.ReadFile(filename)
			must(err)
			if !bytes.Equal(old, data) {
				panic("schema drift: " + filename)
			}
		} else {
			must(os.MkdirAll("schemas", 0755))
			must(os.WriteFile(filename, data, 0644))
		}
	}
	fmt.Printf("PASS: %d generated data-shape schemas\n", len(types))
}

// applyReportProtocol constrains documented wire values. The Go type remains
// authoritative for field names, shapes and presence; command-specific data
// is intentionally open, including when a persisted report carries a summary.
func applyReportProtocol(schema object) {
	props := schema["properties"].(object)
	props["mode"].(object)["enum"] = []string{"static", "local-write"}
	props["operation_status"].(object)["enum"] = []string{"complete", "incomplete"}
	props["result"].(object)["enum"] = []string{"clean", "issues_found", "blocked", "error"}
	props["exit_code"].(object)["enum"] = []int{0, 1, 2, 3, 4}
	props["command_execution"].(object)["const"] = "NOT_RUN"
	props["data"].(object)["description"] = "Command-specific payload. Saved reports can contain less detail than stdout."
	coverage := props["coverage"].(object)["properties"].(object)
	coverage["status"].(object)["enum"] = []string{"complete", "incomplete"}
	finding := props["findings"].(object)["items"].(object)["properties"].(object)
	finding["severity"].(object)["enum"] = []string{"Critical", "High", "Medium", "Low", "Informational"}
	finding["method"].(object)["const"] = "static"
}

func shape(t reflect.Type, metadata bool) object {
	if t.Kind() == reflect.Pointer {
		return shape(t.Elem(), metadata)
	}
	switch t.Kind() {
	case reflect.Struct:
		props := object{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")
			if !f.IsExported() || tag[0] == "-" || metadata && f.Tag.Get("yaml") == "-" {
				continue
			}
			name := tag[0]
			if name == "" {
				name = f.Name
			}
			props[name] = shape(f.Type, metadata)
			if name == "schema_version" {
				props[name] = object{"type": "string", "const": "1.0"}
			}
			if name == "execution_status" {
				props[name] = object{"type": "string", "const": "NOT_RUN"}
			}
			if metadata && name == "severity" {
				props[name] = object{"type": "string", "enum": []string{"P0", "P1", "P2", "P3"}}
			}
			if !strings.Contains(f.Tag.Get("json"), ",omitempty") && f.Tag.Get("schema") != "optional" {
				required = append(required, name)
			}
		}
		return object{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	case reflect.Slice, reflect.Array:
		return object{"type": "array", "items": shape(t.Elem(), metadata)}
	case reflect.Map:
		return object{"type": "object", "additionalProperties": shape(t.Elem(), metadata)}
	case reflect.String:
		return object{"type": "string"}
	case reflect.Bool:
		return object{"type": "boolean"}
	case reflect.Float32, reflect.Float64:
		return object{"type": "number"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return object{"type": "integer"}
	case reflect.Interface:
		return object{}
	default:
		panic("unsupported schema type: " + t.String())
	}
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}

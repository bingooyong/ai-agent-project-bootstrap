package schemas_test

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bingooyong/oh-my-blueprint/internal/cli"
)

//go:embed cli-report.schema.json
var reportSchema []byte

func TestReportEnvelopeMatchesSchema(t *testing.T) {
	var schema reportShape
	if err := json.Unmarshal(reportSchema, &schema); err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, expectedCode int, args ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		args = append(args, "--format", "json")
		if code := cli.Run(context.Background(), args, &stdout, &stderr); code != expectedCode {
			t.Fatalf("exit code = %d, want %d: %s", code, expectedCode, stderr.Bytes())
		}
		schema.check(t, "report", stdout.Bytes())
	}
	t.Run("success without coverage", func(t *testing.T) { run(t, 0, "version") })
	t.Run("invalid arguments", func(t *testing.T) { run(t, 2, "version", "unexpected") })
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return
	}
	t.Run("quality finding and coverage", func(t *testing.T) {
		run(t, 1, "check", t.TempDir(), "--ci")
	})
	t.Run("incomplete scan", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{"), 0600); err != nil {
			t.Fatal(err)
		}
		run(t, 3, "scan", root)
	})
	t.Run("integrity blocked", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, ".ombp"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".ombp", "lock.yaml"), []byte("{"), 0600); err != nil {
			t.Fatal(err)
		}
		run(t, 4, "check", root, "--ci")
	})
	t.Run("saved preview summary", func(t *testing.T) {
		root := t.TempDir()
		run(t, 0, "init", root, "--output", "report.json")
		data, err := os.ReadFile(filepath.Join(root, "report.json"))
		if err != nil {
			t.Fatal(err)
		}
		schema.check(t, "saved report", data)
	})
}

// These are the shape keywords used by this report schema. The test compares
// the generated field constraints with the bytes emitted by real CLI commands.
type reportShape struct {
	Type       string                 `json:"type"`
	Const      json.RawMessage        `json:"const"`
	Enum       []json.RawMessage      `json:"enum"`
	Properties map[string]reportShape `json:"properties"`
	Required   []string               `json:"required"`
	Items      *reportShape           `json:"items"`
}

func (s reportShape) check(t *testing.T, path string, data json.RawMessage) {
	t.Helper()
	var dest any
	switch s.Type {
	case "object":
		dest = new(map[string]json.RawMessage)
	case "array":
		dest = new([]json.RawMessage)
	case "string":
		dest = new(string)
	case "boolean":
		dest = new(bool)
	case "integer":
		dest = new(int64)
	case "":
		return // command-specific data
	default:
		t.Fatalf("%s: unhandled schema type %q", path, s.Type)
	}
	if bytes.Equal(data, []byte("null")) {
		t.Fatalf("%s: expected %s, got null", path, s.Type)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		t.Fatalf("%s: expected %s: %v", path, s.Type, err)
	}
	if s.Const != nil && !bytes.Equal(s.Const, data) {
		t.Errorf("%s: got %s, want %s", path, data, s.Const)
	}
	if len(s.Enum) != 0 {
		matched := false
		for _, allowed := range s.Enum {
			matched = matched || bytes.Equal(allowed, data)
		}
		if !matched {
			t.Errorf("%s: value %s is outside the schema enum", path, data)
		}
	}
	switch value := dest.(type) {
	case *map[string]json.RawMessage:
		for _, name := range s.Required {
			if _, ok := (*value)[name]; !ok {
				t.Errorf("%s: required field %q missing", path, name)
			}
		}
		for name, property := range *value {
			shape, ok := s.Properties[name]
			if !ok {
				t.Errorf("%s: undocumented field %q", path, name)
				continue
			}
			shape.check(t, path+"."+name, property)
		}
	case *[]json.RawMessage:
		if s.Items == nil {
			t.Fatalf("%s: undocumented array item", path)
		}
		for _, item := range *value {
			s.Items.check(t, path+"[]", item)
		}
	}
}

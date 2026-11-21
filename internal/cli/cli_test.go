package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProtocol(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"version", "--format", "json"}, 0},
		{[]string{"version", "unexpected"}, 2},
		{[]string{"version", "--format", "yaml"}, 2},
		{[]string{"unknown"}, 2},
		{[]string{"scan", t.TempDir(), "--format", "json"}, 0},
	} {
		var out, errout bytes.Buffer
		code := Run(context.Background(), tc.args, &out, &errout)
		if code != tc.code {
			t.Errorf("%v code=%d want=%d stderr=%s", tc.args, code, tc.code, errout.String())
		}
		if code == 0 {
			var v report
			if err := json.Unmarshal(out.Bytes(), &v); err != nil {
				t.Fatalf("invalid JSON: %v %s", err, out.String())
			}
			if v.CommandExecution != "NOT_RUN" {
				t.Fatal("manufactured execution")
			}
		}
	}
}

func TestScanDoesNotExecuteScripts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"test":"touch SHOULD_NOT_EXIST"},"dependencies":{"react":"1"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := Run(context.Background(), []string{"scan", dir, "--format", "json"}, &out, &errout); code != 0 {
		t.Fatalf("code=%d %s", code, errout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "SHOULD_NOT_EXIST")); !os.IsNotExist(err) {
		t.Fatal("project script executed")
	}
	if bytes.Contains(out.Bytes(), []byte("touch SHOULD_NOT_EXIST")) {
		t.Fatal("script content echoed")
	}
}

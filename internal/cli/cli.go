// Package cli adapts terminal requests to the offline application services.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/bingooyong/oh-my-blueprint/internal/model"
	"github.com/bingooyong/oh-my-blueprint/internal/rules"
	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
	"github.com/bingooyong/oh-my-blueprint/internal/scanner"
	"github.com/bingooyong/oh-my-blueprint/internal/strictio"
)

var Version = "0.1.0-dev"

type finding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Blocking bool   `json:"blocking"`
	Evidence string `json:"evidence"`
	Method   string `json:"method"`
}

type report struct {
	SchemaVersion    string    `json:"schema_version"`
	Command          string    `json:"command"`
	Mode             string    `json:"mode"`
	OperationStatus  string    `json:"operation_status"`
	Result           string    `json:"result"`
	ExitCode         int       `json:"exit_code"`
	ToolVersion      string    `json:"tool_version"`
	CommandExecution string    `json:"command_execution"`
	Data             any       `json:"data,omitempty"`
	Findings         []finding `json:"findings"`
}

type commandError struct {
	code    int
	message string
}

func (e *commandError) Error() string { return e.message }

// Run never calls os.Exit, making stream and process behavior testable.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	format := "text"
	emitted := false
	ci := false
	root := &cobra.Command{Use: "ombp", Short: "Oh My Blueprint — evidence-based Agent engineering", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().StringVar(&format, "format", "text", "text or json")
	root.PersistentFlags().BoolVar(&ci, "ci", false, "noninteractive static inspection")
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if format != "text" && format != "json" {
			return &commandError{2, "format must be text or json"}
		}
		if err := ctx.Err(); err != nil {
			return &commandError{3, "operation cancelled"}
		}
		return nil
	}
	emit := func(cmd *cobra.Command, data any, findings []finding, code int, incomplete bool) error {
		emitted = true
		status, result := "complete", "clean"
		if incomplete {
			status, result = "incomplete", "error"
		} else if code == 4 {
			result = "blocked"
		} else if code != 0 {
			result = "issues_found"
		}
		if findings == nil {
			findings = []finding{}
		}
		r := report{"1.0", cmd.CommandPath(), "static", status, result, code, Version, "NOT_RUN", data, findings}
		var err error
		if format == "json" {
			enc := json.NewEncoder(stdout)
			enc.SetIndent("", "  ")
			err = enc.Encode(r)
		} else {
			_, err = fmt.Fprintf(stdout, "%s: %s\n", cmd.CommandPath(), result)
			if err == nil && data != nil {
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				err = enc.Encode(data)
			}
			for _, f := range findings {
				if err == nil {
					_, err = fmt.Fprintf(stdout, "%s: %s\n", f.Code, f.Evidence)
				}
			}
		}
		if err != nil {
			return &commandError{3, "could not write command output"}
		}
		if code != 0 {
			return &commandError{code, "inspection reported issues"}
		}
		return nil
	}
	root.AddCommand(&cobra.Command{Use: "version", Short: "Show tool version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return emit(cmd, map[string]string{"version": Version, "go_version": runtime.Version()}, nil, 0, false)
	}})
	var opts = scanner.DefaultOptions()
	scan := &cobra.Command{Use: "scan [path]", Short: "Read manifests without executing project code", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		p, err := scanner.Scan(ctx, target(a), opts)
		if err != nil {
			return emit(cmd, p, []finding{{"SCAN_INCOMPLETE", "High", true, "Static scan could not finish; no runtime claim is available", "static"}}, 3, true)
		}
		return emit(cmd, p, nil, 0, false)
	}}
	scan.Flags().BoolVar(&opts.Deep, "deep", false, "additional static discovery only")
	scan.Flags().StringSliceVar(&opts.Exclude, "exclude", nil, "additional exclusion patterns")
	root.AddCommand(scan)
	root.AddCommand(&cobra.Command{Use: "profile [path]", Short: "Read the stored project profile", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		r, err := safeio.Open(target(a))
		if err != nil {
			return &commandError{3, "could not open project"}
		}
		defer r.Close()
		b, err := r.Read(".ombp/profile.json", 4<<20)
		if err != nil {
			return &commandError{2, "stored project profile is unavailable"}
		}
		var p model.Profile
		if strictio.Decode(b, &p) != nil || p.SchemaVersion != "1.0" {
			return &commandError{2, "stored profile has an invalid or unsupported schema"}
		}
		return emit(cmd, p, nil, 0, false)
	}})
	root.AddCommand(&cobra.Command{Use: "doctor [path]", Short: "Diagnose local platform and project configuration", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		r, err := safeio.Open(target(a))
		if err != nil {
			return emit(cmd, nil, []finding{{"PLATFORM_OR_ROOT_UNAVAILABLE", "High", true, "Strict project I/O is unavailable for this platform or root", "static"}}, 3, true)
		}
		defer r.Close()
		findings := []finding{}
		code := 0
		if _, err := r.Lstat(".ombp/lock.yaml"); errors.Is(err, fs.ErrNotExist) {
			findings = append(findings, finding{"PROJECT_NOT_INITIALIZED", "Medium", false, "No OMBP lock has been created", "static"})
			code = 1
		} else if err != nil {
			return &commandError{3, "project lock inspection failed"}
		}
		if _, err := r.Lstat(".ombp/state/journal.json"); err == nil {
			findings = append(findings, finding{"PENDING_TRANSACTION", "Critical", true, "A transaction journal requires inspection before writes", "static"})
			code = 4
		}
		return emit(cmd, map[string]any{"os": runtime.GOOS, "architecture": runtime.GOARCH, "network_accessed": false, "global_settings_changed": false}, findings, code, false)
	}})
	ruleCmd := &cobra.Command{Use: "rules", Short: "Inspect deterministic engineering rules"}
	ruleCmd.AddCommand(&cobra.Command{Use: "list", Short: "List bundled rules", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		rs, err := rules.Builtin()
		if err != nil {
			return &commandError{3, "bundled rules are invalid"}
		}
		return emit(cmd, rs, nil, 0, false)
	}})
	ruleCmd.AddCommand(&cobra.Command{Use: "show <id>", Short: "Show one bundled rule", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		rs, err := rules.Builtin()
		if err != nil {
			return &commandError{3, "bundled rules are invalid"}
		}
		for _, r := range rs {
			if r.ID == a[0] {
				return emit(cmd, r, nil, 0, false)
			}
		}
		return &commandError{2, "rule ID not found"}
	}})
	ruleCmd.AddCommand(&cobra.Command{Use: "validate [path]", Short: "Validate builtin and project custom rules", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		p, err := scanner.Scan(ctx, target(a), scanner.DefaultOptions())
		if err != nil {
			return &commandError{3, "project scan did not finish"}
		}
		extra, err := rules.LoadCustom(target(a))
		if err != nil {
			return &commandError{4, "custom rule validation failed"}
		}
		resolved, err := rules.Resolve(p, extra)
		if err != nil {
			return &commandError{4, "rule resolution is blocked"}
		}
		return emit(cmd, resolved, nil, 0, false)
	}})
	root.AddCommand(ruleCmd)
	_ = ci // Static commands have no interactive or implicit write path.
	if cmd, err := root.ExecuteContextC(ctx); err != nil {
		code := 2
		var e *commandError
		if errors.As(err, &e) {
			code = e.code
		}
		if format == "json" && !emitted {
			status, result := "complete", "blocked"
			if code == 2 {
				result = "error"
			}
			if code == 3 {
				status, result = "incomplete", "error"
			}
			name := "ombp"
			if cmd != nil {
				name = cmd.CommandPath()
			}
			_ = json.NewEncoder(stdout).Encode(report{"1.0", name, "static", status, result, code, Version, "NOT_RUN", nil, []finding{{"COMMAND_FAILED", "High", code == 4, "Command or input validation failed; no changes were made", "static"}}})
		}
		message := "invalid command or arguments (use --help)"
		if e != nil {
			message = e.message
		}
		_, _ = fmt.Fprintln(stderr, message)
		return code
	}
	return 0
}

func target(args []string) string {
	if len(args) == 0 {
		return "."
	}
	return args[0]
}

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

	"github.com/bingooyong/oh-my-blueprint/internal/governance"
	"github.com/bingooyong/oh-my-blueprint/internal/model"
	"github.com/bingooyong/oh-my-blueprint/internal/rules"
	"github.com/bingooyong/oh-my-blueprint/internal/safeio"
	"github.com/bingooyong/oh-my-blueprint/internal/scanner"
	"github.com/bingooyong/oh-my-blueprint/internal/strictio"
	"github.com/bingooyong/oh-my-blueprint/internal/updater"
)

var Version = "0.1.0-dev"

type finding struct {
	Code                 string               `json:"code"`
	Severity             string               `json:"severity"`
	Blocking             bool                 `json:"blocking"`
	Evidence             string               `json:"evidence"`
	Method               string               `json:"method"`
	RuleIDs              []string             `json:"rule_ids,omitempty"`
	Location             *governance.Location `json:"location,omitempty"`
	Impact               string               `json:"impact,omitempty"`
	Recommendation       string               `json:"recommendation,omitempty"`
	RequiresConfirmation bool                 `json:"requires_confirmation"`
}

// Report is the versioned JSON envelope emitted by the CLI.
type Report struct {
	SchemaVersion    string          `json:"schema_version"`
	Command          string          `json:"command"`
	Mode             string          `json:"mode"`
	OperationStatus  string          `json:"operation_status"`
	Result           string          `json:"result"`
	ExitCode         int             `json:"exit_code"`
	ToolVersion      string          `json:"tool_version"`
	CommandExecution string          `json:"command_execution"`
	Coverage         *model.Coverage `json:"coverage,omitempty"`
	Data             any             `json:"data,omitempty"`
	Findings         []finding       `json:"findings"`
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
	outputPath := ""
	var outputRoot *safeio.Root
	var outputErr *commandError
	defer func() {
		if outputRoot != nil {
			_ = outputRoot.Close()
		}
	}()
	root := &cobra.Command{Use: "ombp", Short: "Oh My Blueprint — evidence-based Agent engineering", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().StringVar(&format, "format", "text", "text or json")
	root.PersistentFlags().BoolVar(&ci, "ci", false, "noninteractive static inspection")
	root.PersistentFlags().StringVar(&outputPath, "output", "", "explicitly create a new report file relative to the target root")
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if format != "text" && format != "json" {
			return &commandError{2, "format must be text or json"}
		}
		if err := ctx.Err(); err != nil {
			return &commandError{3, "operation cancelled"}
		}
		if cmd.Flags().Changed("output") {
			config, _ := cmd.Flags().GetString("config")
			if !validReportPath(outputPath) || sameReportPath(outputPath, config) {
				return &commandError{2, "report output must be a safe relative project path"}
			}
			if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
				return &commandError{2, "dry-run cannot persist a report"}
			}
			projectPath := reportTarget(cmd, args)
			planOut, _ := cmd.Flags().GetString("plan-out")
			if samePlanAndReport(projectPath, outputPath, planOut) {
				return &commandError{2, "plan and report outputs require separate destinations"}
			}
			var err error
			outputRoot, err = safeio.Open(projectPath)
			if err == nil {
				err = outputRoot.CheckCreate(outputPath)
			}
			if err != nil {
				outputErr = reportWriteError(err)
			}
		}
		return nil
	}
	emit := func(cmd *cobra.Command, data any, findings []finding, code int, incomplete bool) error {
		status, result := "complete", "clean"
		if incomplete {
			status, result = "incomplete", "error"
		} else if code == 4 {
			result = "blocked"
		} else if code == 2 {
			result = "error"
		} else if code != 0 {
			result = "issues_found"
		}
		if findings == nil {
			findings = []finding{}
		}
		mode := "static"
		applying, _ := cmd.Flags().GetBool("apply")
		planOut, _ := cmd.Flags().GetString("plan-out")
		if applying || planOut != "" || outputPath != "" {
			mode = "local-write"
		}
		r := Report{SchemaVersion: "1.0", Command: cmd.CommandPath(), Mode: mode, OperationStatus: status,
			Result: result, ExitCode: code, ToolVersion: Version, CommandExecution: "NOT_RUN", Data: data, Findings: findings}
		switch v := data.(type) {
		case model.Profile:
			if cmd.Name() == "scan" {
				r.Coverage = &v.Coverage
			}
		case governance.Report:
			if v.Coverage.Status != "" {
				r.Coverage = &v.Coverage
			}
		}
		if outputPath != "" && code != 2 {
			if outputErr == nil {
				if err := saveReport(outputRoot, outputPath, format, r); err != nil {
					outputErr = reportWriteError(err)
				}
			}
			if outputErr != nil {
				r.Findings = append(r.Findings, finding{Code: "REPORT_WRITE_FAILED", Severity: "High", Blocking: outputErr.code == 4, Evidence: outputErr.message, Method: "static"})
				switch {
				case outputErr.code == 3 || code == 3:
					r.ExitCode, r.OperationStatus, r.Result = 3, "incomplete", "error"
				default:
					r.ExitCode, r.Result = 4, "blocked"
				}
			}
		}
		emitted = true
		if err := writeReport(stdout, format, r); err != nil {
			return &commandError{3, "could not write command output"}
		}
		if outputErr != nil && code != 2 {
			return &commandError{r.ExitCode, outputErr.message}
		}
		if r.ExitCode != 0 {
			return &commandError{r.ExitCode, "inspection reported issues"}
		}
		return nil
	}
	root.AddCommand(&cobra.Command{Use: "version", Short: "Show tool version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return emit(cmd, map[string]string{"version": Version, "go_version": runtime.Version()}, nil, 0, false)
	}})
	var opts = scanner.DefaultOptions()
	scan := &cobra.Command{Use: "scan [path]", Short: "Read manifests without executing project code", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		selection, err := configSelection(cmd)
		if err != nil {
			return err
		}
		cfg, err := projectConfig(target(a), selection)
		if err != nil {
			return err
		}
		options := opts
		options.Exclude = append(cfg.Exclude, opts.Exclude...)
		if err := scanner.ValidateExclusions(opts.Exclude); err != nil {
			return &commandError{2, err.Error()}
		}
		p, err := scanner.Scan(ctx, target(a), options)
		if err != nil {
			return emit(cmd, p, []finding{{Code: "SCAN_INCOMPLETE", Severity: "High", Blocking: true, Evidence: "Static scan could not finish; no runtime claim is available", Method: "static"}}, 3, true)
		}
		return emit(cmd, updater.PrepareProfile(p, cfg, selection), nil, 0, false)
	}}
	scan.Flags().BoolVar(&opts.Deep, "deep", false, "additional static discovery only")
	scan.Flags().StringSliceVar(&opts.Exclude, "exclude", nil, "additional exclusion patterns")
	root.AddCommand(addConfigFlag(scan))
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
		if strictio.Decode(b, &p) != nil || p.SchemaVersion != "1.0" || p.Coverage.Status != "complete" {
			return &commandError{2, "stored profile has an invalid or unsupported schema"}
		}
		return emit(cmd, p, nil, 0, false)
	}})
	root.AddCommand(addConfigFlag(&cobra.Command{Use: "doctor [path]", Short: "Diagnose local platform and project configuration", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		selection, err := configSelection(cmd)
		if err != nil {
			return err
		}
		r, err := safeio.Open(target(a))
		if err != nil {
			return emit(cmd, nil, []finding{{Code: "PLATFORM_OR_ROOT_UNAVAILABLE", Severity: "High", Blocking: true, Evidence: "Strict project I/O is unavailable for this platform or root", Method: "static"}}, 3, true)
		}
		defer r.Close()
		if _, _, err := updater.LoadConfig(r, selection); err != nil {
			return &commandError{2, "project configuration is missing, unsafe or invalid"}
		}
		findings := []finding{}
		code := 0
		lock, lockData, err := updater.LoadLock(r)
		if err != nil {
			return &commandError{4, "project lock is unsafe, invalid or unsupported"}
		}
		if lockData == nil {
			findings = append(findings, finding{Code: "PROJECT_NOT_INITIALIZED", Severity: "Medium", Evidence: "No OMBP lock has been created", Method: "static"})
			code = 1
		} else if selection == "" && lock.ConfigPath != "" {
			return &commandError{2, updater.ErrExplicitConfigRequired.Error()}
		}
		stateUnverified := false
		for _, state := range []struct {
			path, code, evidence string
		}{{".ombp/state/journal.json", "PENDING_TRANSACTION", "A transaction journal requires inspection before writes"},
			{".ombp/state/mutex.json", "WRITE_MUTEX_PRESENT", "A project writer lock requires ownership and recovery review"}} {
			if _, err := r.Lstat(state.path); err == nil {
				findings = append(findings, finding{Code: state.code, Severity: "Critical", Blocking: true, Evidence: state.evidence, Method: "static"})
				code = 4
			} else if !errors.Is(err, fs.ErrNotExist) {
				if !stateUnverified {
					findings = append(findings, finding{Code: "TRANSACTION_STATE_UNVERIFIED", Severity: "High", Blocking: true, Evidence: "Transaction state could not be safely inspected", Method: "static"})
					stateUnverified = true
				}
				code = 3
			}
		}
		return emit(cmd, map[string]any{"os": runtime.GOOS, "architecture": runtime.GOARCH, "network_accessed": false, "global_settings_changed": false}, findings, code, code == 3)
	}}))
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
	ruleCmd.AddCommand(addConfigFlag(&cobra.Command{Use: "validate [path]", Short: "Validate builtin and project custom rules", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		selection, err := configSelection(cmd)
		if err != nil {
			return err
		}
		cfg, err := projectConfig(target(a), selection)
		if err != nil {
			return err
		}
		options := scanner.DefaultOptions()
		options.Exclude = cfg.Exclude
		p, err := scanner.Scan(ctx, target(a), options)
		if err != nil {
			return &commandError{3, "project scan did not finish"}
		}
		projectRules, err := rules.LoadProject(target(a))
		if err != nil {
			return &commandError{4, "custom rule or override validation failed"}
		}
		resolved, err := rules.ResolveWithOverrides(updater.PrepareProfile(p, cfg, selection), projectRules.Rules, projectRules.Overrides)
		if err != nil {
			return &commandError{4, "rule resolution is blocked"}
		}
		return emit(cmd, resolved, nil, 0, false)
	}}))
	root.AddCommand(ruleCmd)
	root.AddCommand(addConfigFlag(&cobra.Command{Use: "check [path]", Short: "Check locked rules and current static project evidence", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		selection, err := configSelection(cmd)
		if err != nil {
			return err
		}
		r, err := governance.CheckWithConfig(ctx, target(a), Version, selection)
		findings := make([]finding, 0, len(r.Findings))
		for _, f := range r.Findings {
			loc := f.Location
			findings = append(findings, finding{Code: f.Code, Severity: f.Severity, Blocking: f.Blocking, Evidence: f.Evidence, Method: f.Method, RuleIDs: f.RuleIDs, Location: &loc, Impact: f.Impact, Recommendation: f.Recommendation, RequiresConfirmation: f.RequiresConfirmation})
		}
		return emit(cmd, r, findings, r.ExitCode, err != nil || r.ExitCode == 3)
	}}))
	addPlanCommands(root, ctx, &ci, emit, func() error {
		if outputErr != nil {
			return outputErr
		}
		return nil
	})
	if cmd, err := root.ExecuteContextC(ctx); err != nil {
		code := 2
		var e *commandError
		if errors.As(err, &e) {
			code = e.code
		}
		failure := finding{Code: "COMMAND_FAILED", Severity: "High", Blocking: code == 4, Evidence: "Command or input validation failed; inspect transaction state before retrying a write", Method: "static"}
		// Some operations fail before producing inspection data. Persist
		// their structured failure once the destination passed preflight.
		if !emitted && cmd != nil && outputRoot != nil && outputErr == nil && code != 2 && ctx.Err() == nil {
			var resultErr *commandError
			if errors.As(emit(cmd, nil, []finding{failure}, code, code == 3), &resultErr) && (outputErr != nil || resultErr.code != code) {
				e, code = resultErr, resultErr.code
			}
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
			_ = json.NewEncoder(stdout).Encode(Report{SchemaVersion: "1.0", Command: name, Mode: "static", OperationStatus: status,
				Result: result, ExitCode: code, ToolVersion: Version, CommandExecution: "NOT_RUN", Findings: []finding{failure}})
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

// The updater is kept outside Cobra's hooks. Flags only choose a reviewed local
// operation; none grants shell, network, production or global-config access.
func addPlanCommands(root *cobra.Command, ctx context.Context, ci *bool, emit func(*cobra.Command, any, []finding, int, bool) error, checkReportOutput func() error) {
	for _, verb := range []string{"init", "diff"} {
		var dryRun, printDiff, apply, scan bool
		var planOut, planIn, agent string
		cmd := &cobra.Command{Use: verb + " [path]", Short: "Preview a bound plan; explicitly apply reviewed local changes", Args: cobra.MaximumNArgs(1)}
		cmd.RunE = func(cmd *cobra.Command, a []string) error {
			if agent != "codex" {
				return &commandError{2, "only the codex adapter is currently implemented"}
			}
			if dryRun && (apply || planOut != "") || apply && (*ci || planIn == "" || planOut != "") || !apply && planIn != "" || verb == "diff" && apply {
				return &commandError{2, "incompatible preview, plan, CI or apply flags"}
			}
			selection, err := configSelection(cmd)
			if err != nil {
				return err
			}
			if apply {
				p, err := updater.ReadPlan(planIn)
				if err != nil {
					return &commandError{2, "reviewed plan could not be read or validated"}
				}
				if err := checkReportOutput(); err != nil {
					return err
				}
				if selection != "" && updater.ConfigLocation(p.ConfigPath) != selection {
					return &commandError{4, "configuration path differs from the reviewed plan"}
				}
				if err := updater.ValidateTarget(target(a), p); err != nil {
					return &commandError{4, "plan belongs to a different project root"}
				}
				if err := updater.Apply(ctx, p, Version); err != nil {
					return &commandError{4, "plan application failed; inspect changes and recovery journal before retrying"}
				}
				return emit(cmd, map[string]any{"applied": true, "changed_files": len(p.Changes), "plan_fingerprint": p.Fingerprint}, nil, 0, false)
			}
			p, err := updater.BuildWithConfig(ctx, target(a), Version, selection)
			if err != nil {
				if errors.Is(err, updater.ErrExplicitConfigRequired) {
					return &commandError{2, updater.ErrExplicitConfigRequired.Error()}
				}
				return &commandError{updater.ExitCode(err), "plan blocked by project configuration, source or output protection; run doctor/check for diagnostics"}
			}
			if planOut != "" {
				if err := checkReportOutput(); err != nil {
					return err
				}
				if err := updater.SavePlan(planOut, p); err != nil {
					return &commandError{4, "plan output already exists or could not be safely created"}
				}
			}
			// Diffs are optional presentation, never an authority to write. A saved
			// plan retains its exact content; the output view may omit verbose diffs.
			if !printDiff && verb != "diff" {
				for i := range p.Changes {
					p.Changes[i].Diff = ""
				}
			}
			return emit(cmd, p, nil, 0, false)
		}
		cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without persistent output")
		cmd.Flags().BoolVar(&printDiff, "print-diff", false, "include planned file diffs")
		cmd.Flags().StringVar(&planOut, "plan-out", "", "explicitly create a new plan file")
		cmd.Flags().StringVar(&agent, "agent", "codex", "target Agent adapter")
		cmd.Flags().BoolVar(&scan, "scan", false, "explicit static scan (already the default)")
		if verb != "diff" {
			cmd.Flags().BoolVar(&apply, "apply", false, "apply the verified --plan")
			cmd.Flags().StringVar(&planIn, "plan", "", "path to reviewed plan")
		}
		root.AddCommand(addConfigFlag(cmd))
	}
	var recoverApply bool
	var journalSHA, mutexSHA string
	recover := &cobra.Command{Use: "recover [path]", Short: "Explicitly recover a pending local file transaction", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		if !recoverApply {
			if cmd.Flags().Changed("journal-sha") || cmd.Flags().Changed("mutex-sha") {
				return &commandError{2, "recovery digests require --apply"}
			}
			review, err := updater.ReviewRecovery(target(a))
			if err != nil {
				return &commandError{4, "no reviewable recovery journal or orphaned writer lock; inspect transaction state"}
			}
			return emit(cmd, review, nil, 0, false)
		}
		if *ci || cmd.Flags().Changed("journal-sha") && cmd.Flags().Changed("mutex-sha") || (journalSHA == "") == (mutexSHA == "") {
			return &commandError{2, "recovery apply requires exactly one reviewed digest (--journal-sha or --mutex-sha), outside CI"}
		}
		if err := checkReportOutput(); err != nil {
			return err
		}
		if mutexSHA != "" {
			if err := updater.RecoverOrphanedMutex(target(a), mutexSHA); err != nil {
				return &commandError{4, "orphaned writer lock was not confirmed removable; inspect transaction state"}
			}
			return emit(cmd, map[string]bool{"orphaned_mutex_removed": true}, nil, 0, false)
		}
		if err := updater.Recover(target(a), journalSHA); err != nil {
			return &commandError{4, "recovery could not complete; preserve journal and inspect file ownership"}
		}
		return emit(cmd, map[string]bool{"recovered": true}, nil, 0, false)
	}}
	recover.Flags().BoolVar(&recoverApply, "apply", false, "authorize conditional transaction recovery")
	recover.Flags().StringVar(&journalSHA, "journal-sha", "", "SHA-256 returned by a reviewed recovery preview")
	recover.Flags().StringVar(&mutexSHA, "mutex-sha", "", "SHA-256 returned by a reviewed orphaned writer lock preview")
	root.AddCommand(recover)
}

func target(args []string) string {
	if len(args) == 0 {
		return "."
	}
	return args[0]
}

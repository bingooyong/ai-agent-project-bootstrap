# OMBP implementation contracts

Status: execution design, 2026-11-20. User authorized planning and implementation. The full goal remains WP-00 through WP-14; these interfaces start the offline core and do not narrow that goal.

## Domain boundary

The module is `github.com/bingooyong/oh-my-blueprint` (target branding; actual remote is unchanged). Go minimum is 1.25 for os.Root operations, local toolchain 1.27.3. Cobra v1.10.2 and go.yaml.in/yaml/v3 v3.0.6 are pinned implementation dependencies. No Viper, database, model SDK or runtime plugin is needed. Typed strict decoding plus business validation is initially used; published JSON Schemas describe supported data, not a claim of an implemented generic Schema engine.

## Shared APIs (owned by leader)

- `internal/model`: `Evidence{Path, Pointer, SHA256}`, `Fact{Kind, Name, Value, Confidence, Status, Evidence}`, `Command{Name, Argv, Cwd, Status, Evidence}`, `Module{Path, Manifest, Languages}`, `Profile{SchemaVersion, Facts, Commands, Modules, AgentConfigs, UnknownItems, Inputs, Coverage}`, `Coverage{Status, FilesVisited, BytesRead, Excluded}`. All serializable fields use snake_case JSON/YAML tags. `Inputs` is map of safe relative manifest path to SHA-256. Profile contains no timestamp or absolute path.
- `internal/safeio`: `Open(root string) (*Root,error)`, `(*Root).Read(path string, limit int64) ([]byte,error)`, `(*Root).Lstat(path string) (fs.FileInfo,error)`, `(*Root).ReadDir(path string) ([]fs.DirEntry,error)`, `(*Root).Close() error`, `ValidatePath(path string) error`, `Sensitive(path string) bool`, `Digest([]byte) string`. Root remains anchored; reads reject symlinks and nonregular files and cap reads. No scanning of credentials. Output writes are a separate later transaction module.
- `internal/strictio`: `Decode(data []byte, out any) error` for one strict YAML/JSON document: unknown keys, missing required fields, duplicates, aliases, depth/node excess, invalid UTF-8 and trailing documents rejected. Fields tagged `omitempty` or `schema:"optional"` may be omitted; null remains invalid. File-size limits enforced by callers. JSON is YAML subset but duplicate and type behavior must be tested.

Plan review refinements: `Fact.Value` always means the detected value; `DeclaredValue`, `DeclaredSource`, `Conflict` retain manual configuration separately and never replace evidence. Command `Status` means declaration quality (declared/candidate/unknown); `ExecutionStatus` is always `NOT_RUN` for scans. Unix safe reads open each directory with descriptor-relative O_NOFOLLOW and the final file nonblocking, reject nonregular descriptors before reading. Other platforms must explicitly report unsupported protection until tested rather than weakening guarantees.

## Scanner (delegated owner)

`scanner.Options{MaxFiles int, MaxBytes int64, MaxFileBytes int64, MaxDepth int, Exclude []string, Deep bool}`, `scanner.DefaultOptions() Options`, `scanner.Scan(ctx context.Context, root string, options Options) (model.Profile,error)`.

Detect Go/Python/TypeScript/JavaScript/React from explicit manifest evidence, declared dependencies for database/framework clues, build/test commands from real definitions, existing agent configs as paths. Static only, no shell/network. Never echo dependency strings or script bodies into facts: dependency keys become names; commands contain only safe invocation (`npm run <script>` etc), not actual script body. Unknown/dynamic constructs remain unknown. Sensitive content and symlinks excluded before opening. Bound traversal, file sizes and parse depth; trigger incomplete/error on limits or malformed relevant manifest. Go version, toolchain and `require` version-token syntax is checked, but module-path major compatibility and matching the declared toolchain to an installed runtime are not. Output sorted and deterministic. Fixture tests assert negative detection, malformed inputs, no symlink traversal and no script execution.

## Rules (delegated owner)

`rules.Rule` has schema_version,id,title,version,category,severity,source(package),scope(modules,paths),when(languages/frameworks/databases/project_types),overridable,dependencies(id/version),conflicts, constraints(key/operator/value),enforcement,validation(kind/description),content; SHA256 computed on canonical normalized rule content.

APIs: `rules.Builtin() ([]Rule,error)`, `rules.Parse([]byte) (Rule,error)`, `rules.LoadCustom(root string) ([]Rule,error)`, `rules.Resolve(profile model.Profile, extra []Rule) (Resolution,error)`, `Resolution{Rules []Rule, Fingerprint string, Warnings []string}`. Resolve includes builtins and supplied custom rules; loading does not happen implicitly inside renderer. Explicit dependency closure/version, cycles, protected duplicate overrides, conflict/typed constraint checks; stable topological sorting. Unknown predicates fail validation. Scope overlap not proven disjoint is treated conservatively; semantic natural language conflicts cannot be certified. Baseline covers fifteen PRD domains, language packs for Go/Python/TS/React and database safety. Root package assets may embed versioned Markdown rules, with no executable templates from third parties.

## CLI and subsequent modules (leader)

`cli.Run(ctx,args,stdout,stderr) int` uses Cobra, exact positional/flag validation and JSON envelope. Scan/profile/rules first; init/diff/check/apply only advertised once functional. Unsupported commands fail clearly, never successful placeholders. CLI is a thin consumer of scanner/rules and later renderer/updater. Errors 0/1/2/3/4 per SDD; no shell execution, remote download or global settings.

Tests run without real secrets, production connections, user global skills, or writes to original evidence. Evidence is in a newly named directory per validation run.

## Integrated preview and write protocol (2026-11-21)

CLI `init` and `diff` consume updater.Build. Explicit `init --apply --plan` rereads a no-follow bounded plan, verifies its root against the requested project and recomputes the plan before mutation. `--dry-run` cannot persist plan/report output; CI cannot apply. `--scan` is a redundant static intent flag, `--agent codex` currently the only implemented target. JSON command execution status refers to project script execution, which remains NOT_RUN even when local configuration files are applied.

Recovery is a separate explicit local operation, never an automatic Check side effect. It must preserve live writers and unknown ownership, reconcile all persisted intents by hashes, and preserve conflicting manual edits. State files are local transaction material, not arbitrary instructions supplied by a repository. Progress and limits of this implementation belong to acceptance evidence; the existence of command scaffolding does not satisfy all of WP-05 or MVP.

Recovery CLI now previews conditional restorations and requires the exact reviewed journal digest for `recover --apply --journal-sha`. A digest binds review to bytes, not provenance or authorization; journal content still requires review. Independent review regressions cover mutable lock hashes hiding omitted rules, unmanaged intermediate instruction budgets, sensitive external-plan parents and user symlink parents. Build rederives rules/output inventory and binds all ancestor AGENTS/override states; Check never trusts editable hashes as the sole proof that required rules exist.

Stored profiles omit traversal counters explicitly (`coverage.counters_omitted=true`), while scan reports retain actual counters. This preserves stable generated bytes and allows a saved plan inside the project without its incidental directory entry making the plan stale. The stable profile fingerprint still includes manifest hashes, declared commands, module evidence and manual declarations. Unix filesystem writes remain cooperative compare-and-replace, not an isolation guarantee against malicious concurrent renames or hardware power loss. Other-platform safety/runtime acceptance remains open.

`--config` selects an existing non-sensitive file under the project root. Plans bind the selected relative path and raw digest, and replay builds from that selection. Locks record an alternate path for future comparisons. Inspection and planning require explicit selection when the lock names an alternate file. `schema_version` and `agents` are required, while `exclude` and `declarations` may be omitted and cannot be null.

`--output` creates a report with no replacement in an existing project directory and leaves product writes governed by their own flags and plan checks. Persisted plan and recovery summaries omit full file bodies and absolute paths. Check reports structured command-source and local instruction-link findings. Link checks query anchored no-follow target metadata and cap aggregate inspection at 8 MiB and 512 candidates.

JavaScript command evidence lists the script declaration first, then any explicit package manager, lockfile and workspace membership evidence that participated in the choice. Implicit npm is a convention and contributes no evidence or declared build-tool fact.

`rules.LoadProject` returns local rules, explicit override records and raw digests from the bytes it parsed. `rules.ResolveWithOverrides` permits one same-scope, same-applicability replacement per overridable P2/P3 ID, bound to exact rule versions and digests. Plans bind all source bytes; the lock and saved validation report retain applied lineage without the free-form notes. Changes to locked versions still require a distinct upgrade protocol.

When the journal is absent, recovery may review an orphaned mutex only for an identified dead owner. `--mutex-sha` binds the separate review to the canonical project root and mutex contents. Apply rechecks absence, owner, and both digests, and removes only the mutex. It shares the cooperative compare-and-remove boundary of journal recovery.

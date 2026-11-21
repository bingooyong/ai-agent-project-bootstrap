# OMBP implementation contracts

Status: execution design, 2026-11-20. User authorized planning and implementation. The full goal remains WP-00 through WP-14; these interfaces start the offline core and do not narrow that goal.

## Domain boundary

The module is `github.com/bingooyong/oh-my-blueprint` (target branding; actual remote is unchanged). Go minimum is 1.25 for os.Root operations, local toolchain 1.27.3. Cobra v1.10.2 and go.yaml.in/yaml/v3 v3.0.6 are pinned implementation dependencies. No Viper, database, model SDK or runtime plugin is needed. Typed strict decoding plus business validation is initially used; published JSON Schemas describe supported data, not a claim of an implemented generic Schema engine.

## Shared APIs (owned by leader)

- `internal/model`: `Evidence{Path, Pointer, SHA256}`, `Fact{Kind, Name, Value, Confidence, Status, Evidence}`, `Command{Name, Argv, Cwd, Status, Evidence}`, `Module{Path, Manifest, Languages}`, `Profile{SchemaVersion, Facts, Commands, Modules, AgentConfigs, UnknownItems, Inputs, Coverage}`, `Coverage{Status, FilesVisited, BytesRead, Excluded}`. All serializable fields use snake_case JSON/YAML tags. `Inputs` is map of safe relative manifest path to SHA-256. Profile contains no timestamp or absolute path.
- `internal/safeio`: `Open(root string) (*Root,error)`, `(*Root).Read(path string, limit int64) ([]byte,error)`, `(*Root).Lstat(path string) (fs.FileInfo,error)`, `(*Root).ReadDir(path string) ([]fs.DirEntry,error)`, `(*Root).Close() error`, `ValidatePath(path string) error`, `Sensitive(path string) bool`, `Digest([]byte) string`. Root remains anchored; reads reject symlinks and nonregular files and cap reads. No scanning of credentials. Output writes are a separate later transaction module.
- `internal/strictio`: `Decode(data []byte, out any) error` for one strict YAML/JSON document: unknown keys, duplicates, aliases, depth/node excess, invalid UTF-8 and trailing documents rejected. File-size limits enforced by callers. JSON is YAML subset but duplicate and type behavior must be tested.

Plan review refinements: `Fact.Value` always means the detected value; `DeclaredValue`, `DeclaredSource`, `Conflict` retain manual configuration separately and never replace evidence. Command `Status` means declaration quality (declared/candidate/unknown); `ExecutionStatus` is always `NOT_RUN` for scans. Unix safe reads open each directory with descriptor-relative O_NOFOLLOW and the final file nonblocking, reject nonregular descriptors before reading. Other platforms must explicitly report unsupported protection until tested rather than weakening guarantees.

## Scanner (delegated owner)

`scanner.Options{MaxFiles int, MaxBytes int64, MaxFileBytes int64, MaxDepth int, Exclude []string, Deep bool}`, `scanner.DefaultOptions() Options`, `scanner.Scan(ctx context.Context, root string, options Options) (model.Profile,error)`.

Detect Go/Python/TypeScript/JavaScript/React from explicit manifest evidence, declared dependencies for database/framework clues, build/test commands from real definitions, existing agent configs as paths. Static only, no shell/network. Never echo dependency strings or script bodies into facts: dependency keys become names; commands contain only safe invocation (`npm run <script>` etc), not actual script body. Unknown/dynamic constructs remain unknown. Sensitive content and symlinks excluded before opening. Bound traversal, file sizes and parse depth; trigger incomplete/error on limits or malformed relevant manifest. Output sorted and deterministic. Fixture tests assert negative detection, malformed inputs, no symlink traversal and no script execution.

## Rules (delegated owner)

`rules.Rule` has schema_version,id,title,version,category,severity,source(package),scope(modules,paths),when(languages/frameworks/databases/project_types),overridable,dependencies(id/version),conflicts, constraints(key/operator/value),enforcement,validation(kind/description),content; SHA256 computed on canonical normalized rule content.

APIs: `rules.Builtin() ([]Rule,error)`, `rules.Parse([]byte) (Rule,error)`, `rules.LoadCustom(root string) ([]Rule,error)`, `rules.Resolve(profile model.Profile, extra []Rule) (Resolution,error)`, `Resolution{Rules []Rule, Fingerprint string, Warnings []string}`. Resolve includes builtins and supplied custom rules; loading does not happen implicitly inside renderer. Explicit dependency closure/version, cycles, protected duplicate overrides, conflict/typed constraint checks; stable topological sorting. Unknown predicates fail validation. Scope overlap not proven disjoint is treated conservatively; semantic natural language conflicts cannot be certified. Baseline covers fifteen PRD domains, language packs for Go/Python/TS/React and database safety. Root package assets may embed versioned Markdown rules, with no executable templates from third parties.

## CLI and subsequent modules (leader)

`cli.Run(ctx,args,stdout,stderr) int` uses Cobra, exact positional/flag validation and JSON envelope. Scan/profile/rules first; init/diff/check/apply only advertised once functional. Unsupported commands fail clearly, never successful placeholders. CLI is a thin consumer of scanner/rules and later renderer/updater. Errors 0/1/2/3/4 per SDD; no shell execution, remote download or global settings.

Tests run without real secrets, production connections, user global skills, or writes to original evidence. Evidence is in a newly named directory per validation run.

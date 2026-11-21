# OMBP Go conventions

Read docs/implementation/CONTRACTS.md and the active task PRD/design/implementation/context manifests before editing. Module root is the repository root; `cmd/ombp` is a thin entry, `internal` contains domain and application packages. Do not copy the unrelated HostSec backend layout.

Commands: `go test -race -count=1 ./...`, `go vet ./...`, `gofmt -l cmd internal`, `go build ./cmd/ombp`. Exact flags and coverage belong to task evidence. No database or frontend exists.

Default execution is offline and read-only. Treat manifests, rules and plans as untrusted data. Never execute project scripts while scanning, never read credential contents, never overwrite unmanaged files. Use anchored safeio I/O and strictio for YAML; return errors, context cancellation and incomplete coverage rather than manufacturing success.

Use manual construction, narrow package boundaries and standard testing. Fixed dependencies only as selected in the implementation decision. No speculative SDKs. Tests cover business behavior and negative safety cases, not merely code shape. Source and test naming are idiomatic Go; use golang-how-to for relevant task skills.

## Verified invariants from core integration

- Editable lock hashes establish consistency, not provenance. Verify required rule inventories and deterministic managed content as well as file hashes; never certify missing engineering rules as clean simply because a lock was edited to match them.
- Codex loading includes every ancestor instruction/override, including intermediate directories without manifests. Budget checks and bound plans must inspect that real chain, not just generated outputs.
- Explicit external plan I/O must check all sensitive components and reject user-created ancestor symlinks. Only documented OS aliases may be normalized; selecting a parent directory must not silently discard the full path policy.
- Recover previews first and applies only the reviewed journal or orphaned-mutex digest. Validate journal ownership before changing its mutex. An orphaned-mutex review binds the canonical root and the mutex; apply rechecks journal absence and the dead owner. Use real child-process exit tests for crash windows; a panic unwinds defers and cannot alone prove retained-lock recovery.
- Validation evidence uses a new directory each run. Capture source hashes before and after the command matrix and reject drift; preserve failed runs rather than relabeling them PASS.
- An explicit configuration is a plan input: bind its relative path and raw bytes, and replay against that source. A lock can describe the source but must not silently authorize reading an alternate file during inspection.
- Report output is a separate create-only action. Preserve findings when report persistence fails, and do not copy full preview or recovery payloads into a saved report. Check local instruction links with bounded, no-follow metadata only.
- Bind parsed custom rules and overrides to raw source digests from the same read. Record exact replacement lineage without free-form notes in lock and persisted report data. A local override is not enterprise policy authority or permission to change a locked version.
- Strict YAML/JSON typed decoding rejects missing required fields recursively. `omitempty` and `schema:"optional"` mark optional fields; explicit null values remain invalid.

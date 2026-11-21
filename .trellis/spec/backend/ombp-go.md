# OMBP Go conventions

Read docs/implementation/CONTRACTS.md and the active task PRD/design/implementation/context manifests before editing. Module root is the repository root; `cmd/ombp` is a thin entry, `internal` contains domain and application packages. Do not copy the unrelated HostSec backend layout.

Commands: `go test -race -count=1 ./...`, `go vet ./...`, `gofmt -l cmd internal`, `go build ./cmd/ombp`. Exact flags and coverage belong to task evidence. No database or frontend exists.

Default execution is offline and read-only. Treat manifests, rules and plans as untrusted data. Never execute project scripts while scanning, never read credential contents, never overwrite unmanaged files. Use anchored safeio I/O and strictio for YAML; return errors, context cancellation and incomplete coverage rather than manufacturing success.

Use manual construction, narrow package boundaries and standard testing. Fixed dependencies only as selected in the implementation decision. No speculative SDKs. Tests cover business behavior and negative safety cases, not merely code shape. Source and test naming are idiomatic Go; use golang-how-to for relevant task skills.

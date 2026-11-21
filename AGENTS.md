# Oh My Blueprint engineering rules

OMBP is an offline-first CLI for evidence-based project discovery, versioned rules, Agent adaptation and ongoing governance. Product scope: docs/01-PRD.md; implementation order: docs/implementation/task-dag.json; current contracts: docs/implementation/CONTRACTS.md.

- Read the active Trellis task, its JSONL context sources and `.trellis/spec/backend/ombp-go.md` before coding. User has authorized planning and implementation; continue safe local work through verification without repeated permission handoffs.
- Commands from module root: `go test -race -count=1 ./...`, `go vet ./...`, `gofmt -l cmd internal`, `go build ./cmd/ombp`. These become executable as the Go foundation lands; do not invent success before then.
- Scan statically; do not execute repository scripts, open secrets, follow symlinks or contact networks/models by default. All unknowns and incomplete coverage must be explicit. Generated AGENTS.md is instruction, not a security sandbox.
- Keep CLI thin, rule resolution deterministic, file access anchored and bounded. Unmanaged/manual content is protected; plan application must verify current source and target hashes. Do not weaken safety to obtain green tests.
- Use native agents for bounded independent lanes where useful. Specify ownership and shared workspace, never revert others; leader owns integration and final evidence. The installed Trellis agent profiles may be unavailable in this App, in which case use a native default/executor with explicit task context and no recursive delegation.
- Respect existing Git changes. Stage explicit paths only. Implementation does not imply remote rename, deployment or publication. Prior commit/push instructions concerned completed changes; keep new phase work reviewable and report its state.
- Preserve Trellis-managed markers. Long-lived verified conventions belong in specs; requirements/design/code/tests/evidence must remain linked. No future command as a successful placeholder. Full goal remains open until all applicable requirements have current evidence.

<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->

# Versioned OMBP rule source

The single executable built-in source is `internal/rules/builtin/*.md`, embedded into the binary. Do not maintain a second copy here. The initial `ombp-core` package contains fifteen governance domains and conditional Go, Python, TypeScript, React and relational database rules. All rule bodies are instructions; metadata validation does not prove implementation compliance or natural-language consistency.

Project custom rules are Markdown files directly under `.ombp/rules/custom/`; they use strict front matter and cannot replace protected rules. Rule package publication and external registries belong to later work packages.

`Rule.Source.Path`, `Rule.Origins` and `Rule.SHA256` are derived output fields; the front matter cannot provide them. Deduplication preserves sorted origins. `Resolution.SelectionReasons` records unconditional applicability, matched evidence and dependency selections. Rule-content digests exclude location metadata; resolution fingerprints include the selected, ordered rules and their sorted provenance/reasons.

Typed constraints describe one scalar property: `equals` and `requires` fix its value, `forbids` excludes a scalar or scalar set, `allows` restricts it to a nonempty scalar set. Strings, booleans and integers are supported; floating-point, nested objects and unknown operators are rejected. Scope globs support `*`, `**` and `?`; uncertain intersections or exhausted analysis budgets block resolution rather than silently omitting protected constraints. Multi-rule set exhaustion is checked conservatively across mutually overlapping scopes and may require human review.

This initial implementation accepts additive custom rules and identical-definition deduplication. It deliberately rejects all same-ID changes: the explicit P2/P3 override-record workflow is not implemented yet. It does not infer semantic agreement between different natural-language bodies, approve P1 exceptions, fetch remote rules or execute validation scripts.

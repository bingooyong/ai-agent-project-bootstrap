# OMBP data-shape schemas

These JSON Schema Draft 2020-12 files are generated from the tagged Go types:

```sh
go run scripts/gen_schema.go
go run scripts/gen_schema.go --check
```

They describe project profile/config, rule front matter, adapter capabilities,
project lock, reviewable plans and the [CLI JSON report envelope](cli-report.schema.json).
The report schema covers common fields, coverage and findings. Its `data` payload
depends on the command; saved reports can contain a summary of the stdout data.
Rule front matter excludes derived body, hash and provenance fields, which cannot
be supplied by a custom rule file.

The CLI uses strict typed decoding plus dedicated business validators. These
files do not replace source hashes, dependency/constraint resolution, protected
rule checks, path safety, managed ownership, plan recomputation or transaction
recovery. No generic JSON Schema engine or remote `$ref` loading is enabled.

Schema freshness is part of `make check`. Runtime conformance to all business
requirements requires the corresponding tests and acceptance evidence; a schema
file existing is not acceptance evidence by itself.

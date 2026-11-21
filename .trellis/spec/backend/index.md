# Backend Development Guidelines

> Best practices for backend development in this project.

---

## Overview

This is the Go CLI layer of Oh My Blueprint. Read [OMBP Go conventions](ombp-go.md) and [implementation contracts](../../../docs/implementation/CONTRACTS.md) before coding. Template topics below are navigation candidates, not verified project facts. No database or web backend exists.

## Pre-Development Checklist

Read the active task artifacts and all context manifest inputs, then ombp-go.md. Use the module root and safeio/strictio boundaries defined in the contracts. Unknown requirements remain explicit; product roadmap stages are not implemented capabilities.

## Quality Check

Run targeted tests, then go test -race -count=1 ./..., go vet ./..., gofmt and build as applicable. Persist exact commands and results in fresh evidence directories; do not label a plan or static document check as runtime PASS.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Module organization and file layout | To fill |
| [Database Guidelines](./database-guidelines.md) | ORM patterns, queries, migrations | To fill |
| [Error Handling](./error-handling.md) | Error types, handling strategies | To fill |
| [Quality Guidelines](./quality-guidelines.md) | Code standards, forbidden patterns | To fill |
| [Logging Guidelines](./logging-guidelines.md) | Structured logging, log levels | To fill |

---

## How to Fill These Guidelines

For each guideline file:

1. Document your project's **actual conventions** (not ideals)
2. Include **code examples** from your codebase
3. List **forbidden patterns** and why
4. Add **common mistakes** your team has made

The goal is to help AI assistants and new team members understand how YOUR project works.

---

**Language**: All documentation should be written in **English**.

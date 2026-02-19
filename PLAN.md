# Improve `sdf init` DX

**Date:** 2026-02-19
**Status:** Approved

## Problem

Running `sdf init status` creates the stack file but leaves the user on `main` with no branch, no context doc, and unclear next steps. The gap between init and productive work is too wide.

## Design

`sdf init` becomes a single-step operation that creates the stack AND the first branch.

### New behavior

```
sdf init <stack-name> [--base <branch>] [--branch <name>] [--json]
```

1. Create `.sdf/` structure + stack file (existing)
2. Create config file (existing)
3. Determine first branch name — defaults to `<stack-name>`, overridden by `--branch`
4. Apply branch prefix (respecting config, same as `RunBranch`)
5. Create git branch and checkout
6. Register node in stack, save
7. Create context doc stub
8. Push tracking branch to origin
9. Print summary + next steps (or JSON if `--json`)

### Human output

```
Initialized stack "status" (base: main)
Created branch "status/status" (based on main)
Context doc: .sdf/context/status/status.md

Next steps:
  sdf context edit     Edit the context doc for this branch
  sdf pr               Create a pull request
  sdf branch <name>    Add another branch to the stack
  sdf status           View stack topology
```

### JSON output (`--json`)

```json
{
  "stack": "status",
  "base": "main",
  "branch": "status/status",
  "context_doc": ".sdf/context/status/status.md",
  "pushed": true
}
```

Warnings (e.g. push failure) become fields rather than stderr in JSON mode.

### What stays the same

- `sdf branch` unchanged — still used for adding subsequent branches
- `--stack` flag kept as alias for positional arg
- All internal packages unchanged (`internal/git`, `internal/stack`, etc.)

### Code changes

Only `cmd/init.go` — inline branch creation using the same packages `RunBranch` uses.

### Edge cases

- Push to origin fails: warn but don't fail (same as `RunBranch`)
- Branch already exists: error with clear message
- `--branch` flag optional, defaults to stack name

---

## Implementation Plan (Stacked Diffs)

We'll use `sdf` itself to build this in three stacked branches, each a reviewable PR.

### Stack: `init-dx`

#### Branch 1: `init-dx/branch-creation`
**Scope:** Core behavior change — `sdf init` creates the first branch.

- Modify `cmd/init.go` to add branch creation after stack init
- Add `--branch` flag for custom branch name (default: stack name)
- Apply prefix logic from `cfgpkg.ApplyPrefix`
- Create git branch, register node, save stack, create context stub, push
- Update human output with summary + next steps
- Update tests

#### Branch 2: `init-dx/json-output`
**Scope:** Add `--json` flag for machine-readable output.

- Add `--json` flag to `cmd/init.go`
- Define result struct, marshal to JSON when flag is set
- Warnings become struct fields instead of stderr
- Add tests for JSON output mode

#### Branch 3: `init-dx/docs`
**Scope:** Documentation updates.

- Update README.md usage examples for new `sdf init` behavior
- Document `--json` flag for agent/scripting use
- Update any help text / usage strings

### Why this ordering

1. **Branch 1** is the core change — reviewable on its own, delivers the main UX improvement
2. **Branch 2** builds on top — adds the agent-friendly output without cluttering the core change
3. **Branch 3** is docs — depends on final behavior being settled

Each branch is independently reviewable and mergeable in order.

# Docs Sync: Codegen + CI Validation

**Date:** 2026-02-20
**Status:** Approved

## Problem

The `www/` documentation site (Astro) and the CLI source code (Go) are completely decoupled. There is no automation to detect when CLI behavior changes without corresponding documentation updates. As commands, flags, config keys, and defaults evolve, docs will silently drift.

## Goals

1. **Codegen:** Generate structured JSON from the CLI source that the Astro site consumes for command reference and config documentation.
2. **CI validation:** Fail CI when the generated JSON is stale or when narrative docs reference nonexistent commands/flags.
3. **Single source of truth:** Command metadata (name, flags, descriptions, examples) lives in Go source as Cobra command definitions. Docs render from that data.

## Decisions

- **Migrate to Cobra** as the CLI framework. This gives us introspectable command metadata (Use, Short, Long, Example, Flags) for free. The current `flag.FlagSet` + `main.go` switch approach requires a separate metadata layer.
- **Codegen outputs JSON** (`www/src/data/cli-reference.json`). Astro components read it and render structured content. Narrative prose stays hand-written in MDX.
- **CI checks two things:** (1) JSON freshness (regenerate and diff), (2) narrative reference validation (scan MDX for `sdf <cmd>` and `--flag` patterns, validate they exist).

## Architecture

### Go changes

#### Cobra migration

Replace the `main.go` switch + `flag.FlagSet` pattern with a Cobra command tree.

**Before:**
```go
// main.go
switch command {
case "sync":
    exitOnErr(cmd.RunSync(args))
}

// cmd/sync.go
func RunSync(args []string) error {
    fs := flag.NewFlagSet("sync", flag.ExitOnError)
    yes := fs.Bool("y", false, "skip confirmation prompt")
    fs.Parse(args)
    // ...
}
```

**After:**
```go
// main.go
func main() { cmd.Execute() }

// cmd/root.go
var rootCmd = &cobra.Command{
    Use:   "sdf",
    Short: "Stacked Diffs Flow — manage chains of dependent PRs",
}
func Execute() { rootCmd.Execute() }

// cmd/sync.go
var syncCmd = &cobra.Command{
    Use:     "sync [stack-name]",
    Short:   "Detect merged PRs and cascade-rebase downstream branches",
    Long:    `...`,
    Example: `  sdf sync -y ...`,
    Annotations: map[string]string{"category": "stack"},
    RunE:    runSync,
}
func init() {
    rootCmd.AddCommand(syncCmd)
    syncCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
    syncCmd.Flags().Bool("continue", false, "resume after manual conflict resolution")
    syncCmd.Flags().String("stack", "", "stack to sync (default: auto-detect)")
    syncCmd.Flags().Bool("with-content", false, "update PR titles and descriptions")
}
func runSync(cmd *cobra.Command, args []string) error { /* same logic */ }
```

**Commands to migrate (12):**
- Stack: init, fetch, branch, sync, merge, status, pr, move
- Navigation: switch
- Config: config (with show/set subcommands)
- Utility: doctor, version (built-in via Cobra)
- Deprecated: register

**Branch shorthand** (`sdf <branch>` → `sdf switch <branch>`): handled via root command's `RunE` that falls through to switch when the argument doesn't match any subcommand.

#### Config metadata

Add `ConfigKeys()` to `internal/config/config.go`:

```go
type ConfigKeyMeta struct {
    Key         string
    Type        string
    Default     string
    Description string
}

func ConfigKeys() []ConfigKeyMeta {
    return []ConfigKeyMeta{
        {"branch_prefix.enabled", "bool", "true", "Enable/disable branch prefix"},
        {"branch_prefix.prefix", "string", "", "Prefix string (empty = use stack_id)"},
        {"branch_prefix.separator", "string", "/", "Separator character"},
        {"pr_title.conventional_commits", "bool", "false", "Enable conventional commit prefixes in PR titles"},
        {"pr_title.ticket_pattern", "string", "", "Regex to extract ticket ID from branch name"},
        {"sync.with_content", "bool", "false", "Auto-update PR titles and descriptions during sync"},
    }
}
```

### Docgen tool

`cmd/docgen/main.go` — standalone program that:

1. Imports `cmd` package to get the root Cobra command
2. Walks the command tree recursively
3. For each command: extracts Use, Short, Long, Example, Deprecated, Annotations, Flags
4. Calls `config.ConfigKeys()` for config metadata
5. Writes JSON to stdout

**Output:** `www/src/data/cli-reference.json`

```json
{
  "generated": "2026-02-20T12:00:00Z",
  "commands": [
    {
      "name": "sync",
      "category": "stack",
      "use": "sync [stack-name]",
      "short": "Detect merged PRs and cascade-rebase downstream branches",
      "long": "...",
      "example": "  sdf sync -y\n  sdf sync --continue",
      "deprecated": "",
      "flags": [
        {"name": "yes", "shorthand": "y", "type": "bool", "default": "false", "description": "skip confirmation prompt"}
      ],
      "subcommands": []
    }
  ],
  "config_keys": [
    {"key": "branch_prefix.enabled", "type": "bool", "default": "true", "description": "Enable/disable branch prefix"}
  ]
}
```

### Astro changes

**New components:**
- `www/src/components/CommandRef.astro` — renders a single command block (usage, description, flags table, examples)
- `www/src/components/ConfigRef.astro` — renders config key table
- `www/src/components/FlagTable.astro` — reusable flags table

**MDX refactoring:**
- `commands.mdx` — structured parts (flags, usage, examples) come from `<CommandRef>`. Narrative prose (conflict resolution flow, workflow descriptions) stays hand-written.
- `configuration.mdx` — config key table from `<ConfigRef>`. Explanatory prose stays hand-written.
- Other docs (getting-started, core-concepts, ai-features) — unchanged, but CI validates their command/flag references.

### CI validation

#### Check 1: Codegen freshness

```yaml
- name: Docs freshness
  run: |
    make docs
    git diff --exit-code www/src/data/cli-reference.json
```

Fails when someone changes a flag but doesn't regenerate the JSON.

#### Check 2: Narrative reference validation

A Go test or script (`cmd/docgen/validate_test.go`) that:

1. Loads the Cobra command tree
2. Scans `www/src/content/docs/*.mdx` files
3. Extracts `` `sdf <word>` `` patterns → validates command exists
4. Extracts `--<flag>` in context → validates flag exists on the associated command
5. Fails with clear messages: "commands.mdx:42 references unknown command 'restack'"

#### Makefile targets

```makefile
.PHONY: docs
docs:
	go run ./cmd/docgen > www/src/data/cli-reference.json

.PHONY: docs-check
docs-check: docs
	git diff --exit-code www/src/data/cli-reference.json
	go test ./cmd/docgen/...
```

#### CI step

```yaml
- name: Docs freshness
  run: make docs-check
```

## What stays the same

- All existing tests pass (behavior is unchanged, only the CLI framework changes)
- `.sdf/` data model unchanged
- `internal/` packages unchanged (except config metadata export)
- Landing page (`index.astro`) unchanged
- Narrative docs content preserved, just rendered via components for structured parts

## File inventory

| File | Change |
|---|---|
| `go.mod` | Add `github.com/spf13/cobra` |
| `main.go` | Simplify to `cmd.Execute()` |
| `cmd/root.go` | New — root command, version, branch shorthand |
| `cmd/init.go` | Migrate to cobra.Command |
| `cmd/branch.go` | Migrate to cobra.Command |
| `cmd/status.go` | Migrate to cobra.Command |
| `cmd/sync.go` | Migrate to cobra.Command |
| `cmd/pr.go` | Migrate to cobra.Command |
| `cmd/move.go` | Migrate to cobra.Command |
| `cmd/fetch.go` | Migrate to cobra.Command |
| `cmd/switch.go` | Migrate to cobra.Command |
| `cmd/config.go` | Migrate to cobra.Command (with subcommands) |
| `cmd/merge.go` | Migrate to cobra.Command |
| `cmd/register.go` | Migrate to cobra.Command (deprecated) |
| `cmd/resolve.go` | Check if needs migration |
| `cmd/docgen/main.go` | New — docgen tool |
| `cmd/docgen/validate_test.go` | New — narrative reference validation |
| `internal/config/config.go` | Add ConfigKeys() |
| `www/src/data/cli-reference.json` | New — generated |
| `www/src/components/CommandRef.astro` | New |
| `www/src/components/ConfigRef.astro` | New |
| `www/src/components/FlagTable.astro` | New |
| `www/src/content/docs/commands.mdx` | Refactor to use components |
| `www/src/content/docs/configuration.mdx` | Refactor to use components |
| `Makefile` | Add docs, docs-check targets |
| `.github/workflows/ci.yml` | Add docs-check step |

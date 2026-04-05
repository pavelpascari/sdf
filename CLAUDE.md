# sdf — Stacked Diffs Flow

CLI tool for managing stacked diffs (chains of dependent PRs) in Git repos.

## Quick Reference

```sh
make test              # run all tests (unit + property + golden)
make build             # build for current platform → bin/sdf
go install ./...       # rebuild + install to GOPATH/bin
make lint              # golangci-lint
make vet               # go vet
```

## Tech Stack

- **Go 1.24**, Cobra for CLI, Charm ecosystem (lipgloss, huh) for terminal UI
- Shells out to `git`, `gh` (GitHub CLI), `claude` (optional, AI conflict resolution)
- Build: Makefile + GoReleaser | Dist: Homebrew tap `pavelpascari/tap/sdf`

## Project Layout

```
main.go                — Entry point, version injection
cmd/                   — Command implementations (one file per command)
  sync.go, restack.go, move.go, merge.go, status.go, pr.go, branch.go,
  switch.go, new.go, fetch.go, ls.go, prune.go, split.go, config.go, doctor.go
  prnav.go             — PR stack navigation (bare URLs, nav hash)
  *_test.go            — Tests alongside commands
internal/
  stack/stack.go       — Core Stack/Node model, LocalState, progress types
  stack/discover.go    — PR dependency graph → chain detection
  git/git.go           — Git CLI wrapper
  gh/gh.go             — GitHub CLI wrapper
  claude/claude.go     — Claude CLI wrapper
  git/record_*.go, gh/record_*.go, claude/record_*.go — Build-tagged spy recording
  spy/                 — JSONL invocation recorder (for E2E tests)
  config/              — Two-tier config (global + repo), prefix logic
  ui/                  — Terminal styling (lipgloss) + prompts (huh)
  render/              — Output bus (text/JSON), progress display
  ops/                 — (planned) Operation executor, step model, recovery
.sdf/                  — Per-repo metadata (gitignored, never pushed)
  config.json          — Repo config
  stacks/<name>.json   — Stack topology + PR metadata
  local.json           — Ephemeral state (progress, sessions)
```

## Branching and Release Strategy

### Active branches

| Branch | Purpose | Releases |
|--------|---------|----------|
| `main` | Stable. Patch fixes, small features. | `v0.3.x` (via release-please) |
| `v0.5-dev` | Operation engine rewrite. Large structural changes. | `v0.5.0-rc.x` (manual tags) |

### Rules

- **Patch fixes go to `main`**. Periodically merge `main` → `v0.5-dev` to stay current.
- **Operation engine work goes to `v0.5-dev`**. Do NOT merge `v0.5-dev` into `main` until the engine is ready for stable release.
- **RC releases**: Tag `v0.5.0-rc.N` from `v0.5-dev` using the `create-release-tag` workflow. GoReleaser builds binaries and creates a GitHub Release but skips the Homebrew tap (pre-release tags are excluded via `skip_upload: auto`).
- **Stable release**: When ready, merge `v0.5-dev` → `main`. Release-please picks up the changes and creates `v0.5.0`.

### How to ship an RC

```sh
# From v0.5-dev branch, trigger via GitHub Actions:
gh workflow run create-release-tag.yml -f version=0.5.0-rc.1

# Users install with:
go install github.com/pavelpascari/sdf@v0.5.0-rc.1
```

### Release pipeline

```
main → release-please (auto PR) → merge → tag v0.3.x → GoReleaser → GitHub Release + Homebrew tap
v0.5-dev → manual tag v0.5.0-rc.N → GoReleaser → GitHub Release only (no Homebrew)
```

## Architecture Decisions

- `.sdf/` is local-only (gitignored), never pushed to remote
- `ParentBranch()` skips merged/closed nodes to find effective parent
- Stack nav uses bare GitHub URLs (auto-rendered with title/status by GitHub)
- Nav hash (SHA-256) prevents redundant PR description API calls
- `IsClean()` uses `--untracked-files=no`
- Status fetches + fast-forwards base for accurate sync detection
- Merged PRs stay in stack JSON as completed (not removed)
- PR descriptions are the source of truth (no context files)

## Command Pattern

All commands use Cobra. The pattern is:

1. Parse flags
2. Find repo root (`stack.FindRoot()`)
3. Load stack(s)
4. Delegate to `internal/` packages for git/gh/claude operations
5. Return structured errors

Mutation commands (sync, restack, move, merge) persist progress to `.sdf/local.json` for crash recovery. Recovery flags: `--continue`, `--abort`, `--quit`.

## Testing

- Tests use temp git repos: `t.TempDir()` + `os.Chdir` + cleanup
- Always run with: `go test -count=1 ./...` (disable test caching)
- Golden file tests: `make test-golden` / `make test-golden-update`
- E2E tests (needs real GitHub repo): `make test-e2e`
- Spy recording: build with `-tags spyrecord` to capture git/gh/claude invocations as JSONL

## Design Philosophy

- **Thin wrapper around git** — guide users toward safe patterns, don't replace git
- Human-first design, agent-compatible (same commands, `--json` flags)
- Squash merge preferred for PRs
- Never `git add .` during conflict resolution — only stage specific files
- When conflicts happen, user resolves with git directly, then re-runs `sdf sync`

## Current Work: Operation Engine (v0.5-dev)

See `docs/PLAN-operation-engine.md` for the full design.

The operation engine standardizes how all sdf commands execute:
- Every command is a sequence of **steps** with typed inputs/outputs
- Steps declare dependencies via named references (`ref("step-id.output-name")`)
- A shared **executor** validates the step graph, runs steps, persists progress, handles recovery
- **Phase boundaries** enforce safety: mutation steps (reversible) → push steps (commit point) → post-push steps (best-effort)
- All commands get `--verbose` (show exact git/gh commands) and `--dry-run` for free

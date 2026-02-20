# Docs Sync Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Migrate the CLI to Cobra, build a docgen tool that emits JSON from the command tree, wire it into Astro components, and add CI checks for freshness + narrative reference validation.

**Architecture:** Cobra provides introspectable command metadata. A `cmd/docgen/main.go` tool walks the Cobra tree and emits `www/src/data/cli-reference.json`. Astro components render structured data from JSON; narrative prose stays hand-written in MDX. CI regenerates JSON and diffs to catch staleness, plus a Go test scans MDX for invalid command/flag references.

**Tech Stack:** Go 1.24 + spf13/cobra, Astro 5 + MDX, GitHub Actions CI

---

## Phase 1: Cobra Foundation

### Task 1: Add Cobra dependency and create root command

**Files:**
- Modify: `go.mod`
- Create: `cmd/root.go`

**Step 1: Add Cobra dependency**

Run: `go get github.com/spf13/cobra@latest`
Expected: go.mod updated with `github.com/spf13/cobra`

**Step 2: Create `cmd/root.go`**

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set from main.go before Execute is called.
var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "sdf",
	Short: "Stacked Diffs Flow — manage chains of dependent PRs",
	Long: `sdf manages stacked diffs (chains of dependent PRs) in Git repos.
It tracks branch dependencies, cascade-rebases after merges, and keeps
PR descriptions in sync.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// SetVersion sets the version string (called from main before Execute).
func SetVersion(v string) {
	version = v
	rootCmd.Version = v
}

// Execute runs the root command. Called from main.go.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// RootCmd returns the root command for use by docgen.
func RootCmd() *cobra.Command {
	return rootCmd
}
```

**Step 3: Verify it compiles**

Run: `go build ./cmd/...`
Expected: no errors (root.go is not wired into main.go yet)

**Step 4: Commit**

```bash
git add cmd/root.go go.mod go.sum
git commit -m "feat: add Cobra dependency and root command"
```

---

### Task 2: Migrate doctor command

The simplest command — no flags, no stack interaction. Good warmup to validate the pattern.

**Files:**
- Create: `cmd/doctor.go`
- Modify: `cmd/root.go` (add import if needed)

**Step 1: Create `cmd/doctor.go`**

Move the `runDoctor()` and `getVersion()` functions from `main.go` into `cmd/doctor.go`, wrapped in a cobra.Command:

```go
package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/pavelpascari/sdf/internal/ui"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that dependencies are available",
	Long:  `Reports the status of git (required), gh (needed for PR operations), and claude (needed for conflict resolution and PR descriptions).`,
	Example: `  sdf doctor`,
	Annotations: map[string]string{"category": "utility"},
	RunE:  runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	fmt.Println("sdf doctor — checking dependencies")
	fmt.Println()
	allOk := true

	if path, err := exec.LookPath("git"); err != nil {
		fmt.Printf("  %s git        not found (required)\n", ui.SymFail)
		allOk = false
	} else {
		ver := getVersion("git", "--version")
		fmt.Printf("  %s git        %s (%s)\n", ui.SymOK, ver, path)
	}

	if path, err := exec.LookPath("gh"); err != nil {
		fmt.Printf("  %s gh         not found (needed for PR operations)\n", ui.Gray.Render("●"))
	} else {
		ver := getVersion("gh", "version")
		fmt.Printf("  %s gh         %s (%s)\n", ui.SymOK, ver, path)
	}

	if path, err := exec.LookPath("claude"); err != nil {
		fmt.Printf("  %s claude     not found (needed for conflict resolution and PR descriptions)\n", ui.Gray.Render("●"))
	} else {
		ver := getVersion("claude", "--version")
		fmt.Printf("  %s claude     %s (%s)\n", ui.SymOK, ver, path)
	}

	fmt.Println()
	if !allOk {
		return fmt.Errorf("missing required dependencies")
	}
	fmt.Println("All required dependencies are available.")
	return nil
}

func getVersion(name string, arg string) string {
	cmd := exec.Command(name, arg)
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	line := strings.Split(strings.TrimSpace(string(out)), "\n")[0]
	return line
}
```

**Step 2: Verify it compiles**

Run: `go build ./cmd/...`
Expected: no errors

**Step 3: Commit**

```bash
git add cmd/doctor.go
git commit -m "feat: migrate doctor command to Cobra"
```

---

### Task 3: Migrate status, branch, and pr commands

Three simple commands with 1-2 flags each.

**Files:**
- Modify: `cmd/status.go`
- Modify: `cmd/branch.go`
- Modify: `cmd/pr.go`

**Step 1: Rewrite `cmd/status.go`**

Keep `RunStatus` as the Cobra RunE. Replace `flag.NewFlagSet` with Cobra flags. The existing function body stays almost identical — just extract flag values from `cmd.Flags()` instead of local `fs` variables.

```go
var statusCmd = &cobra.Command{
	Use:   "status [stack-name]",
	Short: "Show stack topology and sync state",
	Long: `Fetches from origin and fast-forwards the base branch to ensure sync
state is accurate. Shows each branch with its PR number, status, and
whether it needs syncing. Also performs lightweight reconciliation
against GitHub PR states and warns about structural drift.`,
	Example: `  sdf status                        # status of current branch's stack
  sdf status my-feature             # status of a named stack
  sdf status --stack my-feature     # explicit flag form`,
	Annotations: map[string]string{"category": "stack"},
	RunE:        runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().String("stack", "", "stack to show (default: auto-detect)")
}

func runStatus(cmd *cobra.Command, args []string) error {
	stackFlag, _ := cmd.Flags().GetString("stack")

	stackName := stackFlag
	if stackName == "" && len(args) > 0 {
		stackName = args[0]
	}

	// ... rest of existing RunStatus body from line 24 onward ...
}
```

Pattern: remove the old `RunStatus(args []string) error` signature, replace with `runStatus(cmd *cobra.Command, args []string) error`. Use `cmd.Flags().GetString("stack")` instead of `fs.String("stack", ...)`.

**Step 2: Rewrite `cmd/branch.go`**

Same pattern. Flags: `--stack`, `--no-prefix`. Positional arg: branch name.

```go
var branchCmd = &cobra.Command{
	Use:   "branch <name>",
	Short: "Add a new branch to the current stack",
	Long: `The branch is inserted at the current checkout position in the stack.
If downstream branches exist, their PR base is updated on GitHub automatically.`,
	Example: `  sdf branch repository              # inserts after current branch
  sdf branch --no-prefix api-layer   # skip configured branch prefix`,
	Annotations: map[string]string{"category": "stack"},
	Args:        cobra.ExactArgs(1),
	RunE:        runBranch,
}

func init() {
	rootCmd.AddCommand(branchCmd)
	branchCmd.Flags().String("stack", "", "stack to add the branch to (default: auto-detect)")
	branchCmd.Flags().Bool("no-prefix", false, "create branch without applying the configured prefix")
}

func runBranch(cmd *cobra.Command, args []string) error {
	stackFlag, _ := cmd.Flags().GetString("stack")
	noPrefix, _ := cmd.Flags().GetBool("no-prefix")
	branchName := args[0]
	// ... rest of existing RunBranch body from line 26 onward ...
}
```

**Step 3: Rewrite `cmd/pr.go`**

Flags: `--title`, `--json`.

```go
var prCmd = &cobra.Command{
	Use:   "pr",
	Short: "Create a GitHub PR for the current branch",
	Long: `The PR is automatically based on the parent branch in the stack.
Stack navigation links are added to all PR descriptions after creation.`,
	Example: `  sdf pr                             # auto-generated title and body
  sdf pr --title "Add users table"   # override title
  sdf pr --json                      # machine-readable output`,
	Annotations: map[string]string{"category": "stack"},
	RunE:        runPR,
}

func init() {
	rootCmd.AddCommand(prCmd)
	prCmd.Flags().String("title", "", "PR title (default: auto-generated from branch name)")
	prCmd.Flags().Bool("json", false, "output result as JSON")
}
```

**Step 4: Verify compilation**

Run: `go build ./cmd/...`
Expected: no errors

**Step 5: Commit**

```bash
git add cmd/status.go cmd/branch.go cmd/pr.go
git commit -m "feat: migrate status, branch, and pr commands to Cobra"
```

---

### Task 4: Migrate init command

More complex — has `RunInitWithOutput` variant and 4 flags + positional arg.

**Files:**
- Modify: `cmd/init.go`

**Step 1: Rewrite `cmd/init.go`**

Keep `InitResult` struct and `runInit` core logic. Add cobra.Command.

```go
var initCmd = &cobra.Command{
	Use:   "init [stack-name]",
	Short: "Create a new stack and its first branch",
	Long: `The stack name can be provided as a positional argument or via the --stack flag.
If --branch is omitted, the first branch is named after the stack. The base branch
is auto-detected from origin HEAD if not specified.`,
	Example: `  sdf init my-feature                                # positional stack name
  sdf init --stack my-feature                        # explicit flag
  sdf init my-feature --branch db-schema             # custom first branch name
  sdf init my-feature --base develop                 # custom base branch
  sdf init my-feature --json                         # machine-readable output`,
	Annotations: map[string]string{"category": "stack"},
	RunE:        runInitCmd,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().String("stack", "", "name for the stack (alternative to positional arg)")
	initCmd.Flags().String("base", "", "base branch (default: auto-detected from origin HEAD)")
	initCmd.Flags().String("branch", "", "name for the first branch (default: stack name)")
	initCmd.Flags().Bool("json", false, "output machine-readable JSON")
}
```

The `runInitCmd` function extracts flags from cobra, resolves the stack name from `--stack` flag or positional arg, then delegates to existing `runInit` logic (renamed to avoid collision). Keep `RunInitWithOutput` as a public wrapper for any external callers (like tests).

**Step 2: Update tests in `cmd/init_test.go`**

Tests currently call `RunInit([]string{...})`. Provide a compatibility wrapper or update to use `rootCmd.SetArgs(...)`. The simplest approach: keep a thin `RunInit` that configures and executes the cobra command:

```go
// RunInit runs the init command programmatically (used by tests).
func RunInit(args []string) error {
	rootCmd.SetArgs(append([]string{"init"}, args...))
	return rootCmd.Execute()
}
```

**Step 3: Verify tests pass**

Run: `go test ./cmd/... -run TestInit -count=1 -v`
Expected: all init tests pass

**Step 4: Commit**

```bash
git add cmd/init.go cmd/init_test.go
git commit -m "feat: migrate init command to Cobra"
```

---

### Task 5: Migrate sync command

The largest command. Has 4 flags and complex logic, but the internal functions (`runSyncFull`, `runSyncContinue`, `runSyncFrom`, etc.) stay unchanged.

**Files:**
- Modify: `cmd/sync.go`

**Step 1: Add cobra.Command to `cmd/sync.go`**

```go
var syncCmd = &cobra.Command{
	Use:   "sync [stack-name]",
	Short: "Detect merged PRs and cascade-rebase downstream branches",
	Long: `Fetches from origin, queries GitHub for PR status, reconciles PR states,
cascade-rebases downstream branches, pushes, and updates PR navigation links.

When a rebase conflict occurs, an interactive menu offers Claude resolution,
manual resolution (pausing sync), skip, or abort.`,
	Example: `  sdf sync                          # sync the stack of the current branch
  sdf sync my-feature               # sync a specific stack by name
  sdf sync -y                       # skip confirmation prompt
  sdf sync --continue               # resume after manual conflict resolution
  sdf sync --with-content           # also update PR titles and descriptions`,
	Annotations: map[string]string{"category": "stack"},
	RunE:        runSyncCmd,
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
	syncCmd.Flags().Bool("continue", false, "resume after manual conflict resolution")
	syncCmd.Flags().String("stack", "", "stack to sync (default: auto-detect)")
	syncCmd.Flags().Bool("with-content", false, "update PR titles and descriptions")
}

func runSyncCmd(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	cont, _ := cmd.Flags().GetBool("continue")
	stackFlag, _ := cmd.Flags().GetString("stack")
	withContent, _ := cmd.Flags().GetBool("with-content")

	stackName := stackFlag
	if stackName == "" && len(args) > 0 {
		stackName = args[0]
	}

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	if cont {
		return runSyncContinue(root)
	}

	return runSyncFull(root, stackName, yes, withContent)
}
```

Remove the old `RunSync(args []string) error` function. Keep all internal functions unchanged.

**Step 2: Add test compatibility wrapper**

```go
// RunSync runs the sync command programmatically (used by tests).
func RunSync(args []string) error {
	rootCmd.SetArgs(append([]string{"sync"}, args...))
	return rootCmd.Execute()
}
```

**Step 3: Verify tests pass**

Run: `go test ./cmd/... -run TestSync -count=1 -v`
Expected: all sync tests pass

**Step 4: Commit**

```bash
git add cmd/sync.go cmd/sync_test.go
git commit -m "feat: migrate sync command to Cobra"
```

---

### Task 6: Migrate merge, fetch, and move commands

**Files:**
- Modify: `cmd/merge.go`
- Modify: `cmd/fetch.go`
- Modify: `cmd/move.go`

**Step 1: Rewrite `cmd/merge.go`**

Flags: `--stack`, `-y`, `--method`.

```go
var mergeCmd = &cobra.Command{
	Use:   "merge",
	Short: "Merge head PR and sync remaining branches",
	Long: `Merges the first open PR in the stack, retargets the next PR's base,
then triggers a sync to cascade-rebase remaining branches.`,
	Example: `  sdf merge                          # merge with squash (default)
  sdf merge -y                       # skip confirmation
  sdf merge --method merge           # use regular merge
  sdf merge --method rebase          # use rebase merge
  sdf merge --stack my-feature       # target a specific stack`,
	Annotations: map[string]string{"category": "stack"},
	RunE:        runMergeCmd,
}

func init() {
	rootCmd.AddCommand(mergeCmd)
	mergeCmd.Flags().String("stack", "", "stack to merge (default: auto-detect)")
	mergeCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
	mergeCmd.Flags().String("method", "squash", "merge method: squash, merge, or rebase")
}
```

**Step 2: Rewrite `cmd/fetch.go`**

Flags: `--stack`, `--base`.

**Step 3: Rewrite `cmd/move.go`**

No flags, just positional args (commits). Use `cobra.MinimumNArgs(1)`.

```go
var moveCmd = &cobra.Command{
	Use:   "move <commit>...",
	Short: "Move commits from current branch to parent",
	Long: `Cherry-picks listed commits onto the parent branch, strips them from the
current branch via rebase, and cascade-rebases downstream branches.`,
	Example: `  sdf move abc1234                   # move one commit
  sdf move abc1234 def5678           # move multiple commits`,
	Annotations: map[string]string{"category": "stack"},
	Args:        cobra.MinimumNArgs(1),
	RunE:        runMoveCmd,
}
```

**Step 4: Verify tests pass**

Run: `go test ./cmd/... -count=1 -v`
Expected: all tests pass

**Step 5: Commit**

```bash
git add cmd/merge.go cmd/fetch.go cmd/move.go
git commit -m "feat: migrate merge, fetch, and move commands to Cobra"
```

---

### Task 7: Migrate switch, register, and config commands

**Files:**
- Modify: `cmd/switch.go`
- Modify: `cmd/register.go`
- Modify: `cmd/config.go`

**Step 1: Rewrite `cmd/switch.go`**

No flags. Positional arg optional.

```go
var switchCmd = &cobra.Command{
	Use:   "switch [branch]",
	Short: "Switch to a branch and report its stack",
	Long: `Without arguments, lists all branches across all stacks.
With a branch name, checks it out and shows its stack position.`,
	Example: `  sdf switch db-schema              # switch to a specific branch
  sdf switch                        # list all branches from all stacks`,
	Annotations: map[string]string{"category": "navigation"},
	RunE:        runSwitchCmd,
}
```

Keep `TrySwitch` as a public function — it's used by the root command's fallback.

**Step 2: Rewrite `cmd/register.go`**

Mark as deprecated via Cobra's `Deprecated` field:

```go
var registerCmd = &cobra.Command{
	Use:        "register",
	Short:      "Discover and sync PR stacks from GitHub",
	Deprecated: "use `sdf fetch` instead",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runFetchCmd(cmd, args)
	},
}
```

**Step 3: Rewrite `cmd/config.go`**

Config has subcommands: `show` and `set`. In Cobra:

```go
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage sdf configuration",
	Annotations: map[string]string{"category": "config"},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display effective merged configuration",
	RunE:  runConfigShowCmd,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Example: `  sdf config set branch_prefix.enabled true
  sdf config set --global pr_title.conventional_commits true`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSetCmd,
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetCmd)
	configSetCmd.Flags().Bool("global", false, "set value in global config (~/.config/sdf/config.json)")
}
```

**Step 4: Verify all tests pass**

Run: `go test ./cmd/... -count=1 -v`
Expected: all tests pass

**Step 5: Commit**

```bash
git add cmd/switch.go cmd/register.go cmd/config.go
git commit -m "feat: migrate switch, register, and config commands to Cobra"
```

---

### Task 8: Update main.go and wire branch shorthand

**Files:**
- Modify: `main.go`

**Step 1: Rewrite `main.go`**

Replace the entire switch statement with Cobra's Execute:

```go
package main

import "github.com/pavelpascari/sdf/cmd"

var version = "dev"

func main() {
	cmd.SetVersion(version)
	cmd.Execute()
}
```

**Step 2: Handle branch shorthand**

The current `main.go` handles `sdf <branch>` by trying `cmd.TrySwitch(command)` when the command is unknown. In Cobra, this is handled by setting a `RunE` on the root command that catches unrecognized positional args:

In `cmd/root.go`, update the root command:

```go
var rootCmd = &cobra.Command{
	Use:           "sdf",
	Short:         "Stacked Diffs Flow — manage chains of dependent PRs",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			if err := TrySwitch(args[0]); err == nil {
				return nil
			}
		}
		return cmd.Help()
	},
}
```

Also set `rootCmd.TraverseChildren = true` and configure `rootCmd.FParseErrWhitelist` if needed to let unknown subcommands fall through.

**Step 3: Verify the full binary works**

Run: `go install ./... && sdf help`
Expected: shows Cobra-formatted help with all commands listed

Run: `go install ./... && sdf doctor`
Expected: same output as before

Run: `go install ./... && sdf version`
Expected: prints version

**Step 4: Run full test suite**

Run: `go test ./... -count=1`
Expected: all tests pass

**Step 5: Commit**

```bash
git add main.go cmd/root.go
git commit -m "feat: wire Cobra root command and branch shorthand in main.go"
```

---

### Task 9: Verify Cobra migration end-to-end

**Files:** None (verification only)

**Step 1: Run full test suite**

Run: `go test ./... -count=1 -v`
Expected: all tests pass

**Step 2: Build and install**

Run: `go install ./...`
Expected: clean build

**Step 3: Manual smoke test**

Run each command with `--help` to verify Cobra help output:

```bash
sdf --help
sdf init --help
sdf sync --help
sdf config --help
sdf config set --help
```

Expected: Cobra-formatted help for each command showing usage, flags, examples.

**Step 4: Commit any fixups**

If any tests needed adjustment, commit them here.

---

## Phase 2: Docgen Tool

### Task 10: Add ConfigKeys() metadata to config package

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Add ConfigKeyMeta type and ConfigKeys function**

Append to `internal/config/config.go`:

```go
// ConfigKeyMeta describes a single configuration key for documentation.
type ConfigKeyMeta struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

// ConfigKeys returns metadata for all configuration keys.
func ConfigKeys() []ConfigKeyMeta {
	return []ConfigKeyMeta{
		{"branch_prefix.enabled", "bool", "true", "Enable/disable branch prefix enforcement"},
		{"branch_prefix.prefix", "string", "", "Prefix string prepended to branch names (empty = use stack ID)"},
		{"branch_prefix.separator", "string", "/", "Separator character between prefix and branch name"},
		{"pr_title.conventional_commits", "bool", "false", "Enable conventional commit prefixes in PR titles"},
		{"pr_title.ticket_pattern", "string", "", "Regex to extract ticket ID from branch name for PR titles"},
		{"sync.with_content", "bool", "false", "Auto-update PR titles and descriptions during sync"},
	}
}
```

**Step 2: Write a test**

Create `internal/config/config_keys_test.go`:

```go
package config

import "testing"

func TestConfigKeysNotEmpty(t *testing.T) {
	keys := ConfigKeys()
	if len(keys) == 0 {
		t.Fatal("ConfigKeys() returned empty slice")
	}
	for _, k := range keys {
		if k.Key == "" || k.Type == "" || k.Description == "" {
			t.Errorf("incomplete config key metadata: %+v", k)
		}
	}
}
```

**Step 3: Run test**

Run: `go test ./internal/config/... -count=1 -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/config/config.go internal/config/config_keys_test.go
git commit -m "feat: add ConfigKeys() metadata for docs generation"
```

---

### Task 11: Create docgen tool

**Files:**
- Create: `cmd/docgen/main.go`

**Step 1: Create `cmd/docgen/main.go`**

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/pavelpascari/sdf/cmd"
	"github.com/pavelpascari/sdf/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type CLIReference struct {
	Generated  string       `json:"generated"`
	Commands   []CommandDoc `json:"commands"`
	ConfigKeys []config.ConfigKeyMeta `json:"config_keys"`
}

type CommandDoc struct {
	Name        string       `json:"name"`
	Category    string       `json:"category"`
	Use         string       `json:"use"`
	Short       string       `json:"short"`
	Long        string       `json:"long,omitempty"`
	Example     string       `json:"example,omitempty"`
	Deprecated  string       `json:"deprecated,omitempty"`
	Flags       []FlagDoc    `json:"flags,omitempty"`
	Subcommands []CommandDoc `json:"subcommands,omitempty"`
}

type FlagDoc struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

func main() {
	root := cmd.RootCmd()

	ref := CLIReference{
		Generated:  time.Now().UTC().Format(time.RFC3339),
		Commands:   extractCommands(root),
		ConfigKeys: config.ConfigKeys(),
	}

	data, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}

func extractCommands(parent *cobra.Command) []CommandDoc {
	var docs []CommandDoc
	for _, c := range parent.Commands() {
		if c.Hidden {
			continue
		}
		doc := CommandDoc{
			Name:       c.Name(),
			Category:   c.Annotations["category"],
			Use:        c.Use,
			Short:      c.Short,
			Long:       c.Long,
			Example:    c.Example,
			Deprecated: c.Deprecated,
		}

		c.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Hidden {
				return
			}
			doc.Flags = append(doc.Flags, FlagDoc{
				Name:        f.Name,
				Shorthand:   f.Shorthand,
				Type:        f.Value.Type(),
				Default:     f.DefValue,
				Description: f.Usage,
			})
		})

		doc.Subcommands = extractCommands(c)
		docs = append(docs, doc)
	}
	return docs
}
```

**Step 2: Verify it compiles and runs**

Run: `go run ./cmd/docgen | head -30`
Expected: JSON output with commands array populated

**Step 3: Generate the initial JSON file**

Run: `go run ./cmd/docgen > www/src/data/cli-reference.json`

**Step 4: Verify JSON content**

Read `www/src/data/cli-reference.json` and verify it contains all commands (init, branch, sync, etc.) with their flags.

**Step 5: Commit**

```bash
mkdir -p www/src/data
git add cmd/docgen/main.go www/src/data/cli-reference.json
git commit -m "feat: add docgen tool and initial CLI reference JSON"
```

---

### Task 12: Add Makefile targets and CI step

**Files:**
- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`

**Step 1: Add Makefile targets**

Append to `Makefile`:

```makefile
# ── Documentation ─────────────────────────────────────────────────

# Generate CLI reference JSON from Cobra command tree
.PHONY: docs
docs:
	go run ./cmd/docgen > www/src/data/cli-reference.json

# Check that generated docs are fresh and narrative references are valid
.PHONY: docs-check
docs-check:
	@echo "Checking CLI reference freshness..."
	@go run ./cmd/docgen > www/src/data/cli-reference.json.tmp
	@diff -q www/src/data/cli-reference.json.tmp www/src/data/cli-reference.json > /dev/null 2>&1 \
		&& rm www/src/data/cli-reference.json.tmp \
		|| (rm www/src/data/cli-reference.json.tmp && echo "CLI reference is stale. Run 'make docs' to update." && exit 1)
	@echo "CLI reference is up to date."
```

Note: we compare content ignoring the `generated` timestamp field. The diff approach works because we compare the tmp vs committed file. We should strip the `generated` field before comparison, OR use a fixed/omitted timestamp in CI mode. Simplest: have `docgen` accept a `--no-timestamp` flag that omits the field, then use that in `docs-check`.

Update docgen to accept `--no-timestamp`:

```go
// In cmd/docgen/main.go
noTimestamp := false
for _, arg := range os.Args[1:] {
	if arg == "--no-timestamp" {
		noTimestamp = true
	}
}

ref := CLIReference{
	Commands:   extractCommands(root),
	ConfigKeys: config.ConfigKeys(),
}
if !noTimestamp {
	ref.Generated = time.Now().UTC().Format(time.RFC3339)
}
```

Updated Makefile:

```makefile
.PHONY: docs
docs:
	go run ./cmd/docgen > www/src/data/cli-reference.json

.PHONY: docs-check
docs-check:
	@echo "Checking CLI reference freshness..."
	@go run ./cmd/docgen --no-timestamp > /tmp/sdf-cli-reference-check.json
	@go run ./cmd/docgen --no-timestamp < /dev/null > /dev/null 2>&1  # warm build cache
	@# Compare ignoring generated timestamp
	@jq 'del(.generated)' www/src/data/cli-reference.json > /tmp/sdf-cli-reference-current.json 2>/dev/null || cp www/src/data/cli-reference.json /tmp/sdf-cli-reference-current.json
	@diff -q /tmp/sdf-cli-reference-check.json /tmp/sdf-cli-reference-current.json > /dev/null 2>&1 \
		|| (echo "ERROR: CLI reference is stale. Run 'make docs' to update." && exit 1)
	@echo "CLI reference is up to date."
```

Actually, simplest approach — just strip the `generated` field from both and compare:

```makefile
.PHONY: docs-check
docs-check:
	@echo "Checking CLI reference freshness..."
	@go run ./cmd/docgen --no-timestamp | diff - <(jq 'del(.generated)' www/src/data/cli-reference.json) > /dev/null 2>&1 \
		|| (echo "ERROR: CLI reference is stale. Run 'make docs' and commit." && exit 1)
	@echo "CLI reference is up to date."
```

**Step 2: Add CI step**

Append to `.github/workflows/ci.yml` after the Build step:

```yaml
      - name: Docs check
        run: make docs-check
```

**Step 3: Verify locally**

Run: `make docs-check`
Expected: "CLI reference is up to date."

Modify a flag description in a cmd file, run again:
Expected: "ERROR: CLI reference is stale."

**Step 4: Commit**

```bash
git add Makefile .github/workflows/ci.yml cmd/docgen/main.go
git commit -m "feat: add docs-check CI step and Makefile targets"
```

---

## Phase 3: Narrative Reference Validation

### Task 13: Create reference validation test

**Files:**
- Create: `cmd/docgen/validate_test.go`

**Step 1: Create the validation test**

```go
package main_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/cmd"
	"github.com/spf13/cobra"
)

// TestMDXCommandReferences scans all .mdx files in www/src/content/docs/
// and validates that every `sdf <cmd>` reference matches a real command.
func TestMDXCommandReferences(t *testing.T) {
	root := cmd.RootCmd()
	knownCommands := collectCommandNames(root)

	docsDir := filepath.Join("..", "..", "www", "src", "content", "docs")
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		t.Skipf("cannot read docs dir %s: %v (skipping — run from repo root)", docsDir, err)
		return
	}

	cmdPattern := regexp.MustCompile("`sdf ([a-z-]+)`")

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".mdx") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(docsDir, e.Name()))
		if err != nil {
			t.Errorf("cannot read %s: %v", e.Name(), err)
			continue
		}

		lines := strings.Split(string(data), "\n")
		for lineNum, line := range lines {
			matches := cmdPattern.FindAllStringSubmatch(line, -1)
			for _, m := range matches {
				cmdName := m[1]
				if !knownCommands[cmdName] {
					t.Errorf("%s:%d references unknown command `sdf %s`", e.Name(), lineNum+1, cmdName)
				}
			}
		}
	}
}

func collectCommandNames(parent *cobra.Command) map[string]bool {
	names := map[string]bool{}
	for _, c := range parent.Commands() {
		names[c.Name()] = true
		for _, sub := range c.Commands() {
			names[c.Name()+" "+sub.Name()] = true
		}
	}
	// Also add special aliases
	names["help"] = true
	names["version"] = true
	return names
}
```

**Step 2: Run the test from repo root**

Run: `go test ./cmd/docgen/... -count=1 -v`
Expected: PASS (all referenced commands exist)

**Step 3: Verify it catches bad references**

Temporarily add `` `sdf restack` `` to a .mdx file, run:
Expected: FAIL with "references unknown command `sdf restack`"
Revert the change.

**Step 4: Add to docs-check Makefile target**

Update `docs-check` in Makefile to also run the validation:

```makefile
.PHONY: docs-check
docs-check:
	@echo "Checking CLI reference freshness..."
	@# ... freshness check as before ...
	@echo "Validating narrative references..."
	go test ./cmd/docgen/... -count=1
```

**Step 5: Commit**

```bash
git add cmd/docgen/validate_test.go Makefile
git commit -m "feat: add narrative reference validation for docs"
```

---

## Phase 4: Astro Integration

### Task 14: Create CommandRef Astro component

**Files:**
- Create: `www/src/components/CommandRef.astro`
- Create: `www/src/components/FlagTable.astro`

**Step 1: Create FlagTable component**

```astro
---
interface Props {
  flags: Array<{
    name: string;
    shorthand?: string;
    type: string;
    default: string;
    description: string;
  }>;
}

const { flags } = Astro.props;
---

{flags && flags.length > 0 && (
  <table>
    <thead>
      <tr>
        <th>Flag</th>
        <th>Default</th>
        <th>Description</th>
      </tr>
    </thead>
    <tbody>
      {flags.map(f => (
        <tr>
          <td>
            <code>{f.shorthand ? `-${f.shorthand}, ` : ''}{`--${f.name}`}</code>
          </td>
          <td>{f.default === '' ? '--' : <code>{f.default}</code>}</td>
          <td>{f.description}</td>
        </tr>
      ))}
    </tbody>
  </table>
)}
```

**Step 2: Create CommandRef component**

```astro
---
import FlagTable from './FlagTable.astro';

interface Props {
  command: {
    name: string;
    use: string;
    short: string;
    long?: string;
    example?: string;
    deprecated?: string;
    flags?: Array<any>;
    subcommands?: Array<any>;
  };
}

const { command } = Astro.props;
---

<h3><code>sdf {command.use}</code></h3>

{command.deprecated && (
  <p><strong>Deprecated:</strong> {command.deprecated}</p>
)}

<p>{command.short}</p>

{command.long && <p>{command.long}</p>}

{command.example && (
  <pre><code class="language-bash">{command.example}</code></pre>
)}

{command.flags && command.flags.length > 0 && (
  <>
    <p>Flags:</p>
    <FlagTable flags={command.flags} />
  </>
)}

{command.subcommands && command.subcommands.length > 0 && (
  command.subcommands.map(sub => (
    <div>
      <h4><code>sdf {command.name} {sub.use}</code></h4>
      <p>{sub.short}</p>
      {sub.example && (
        <pre><code class="language-bash">{sub.example}</code></pre>
      )}
      {sub.flags && sub.flags.length > 0 && <FlagTable flags={sub.flags} />}
    </div>
  ))
)}

<hr />
```

**Step 3: Verify Astro builds**

Run: `cd www && npm run build`
Expected: build succeeds (components are created but not yet used)

**Step 4: Commit**

```bash
git add www/src/components/CommandRef.astro www/src/components/FlagTable.astro
git commit -m "feat: add CommandRef and FlagTable Astro components"
```

---

### Task 15: Create ConfigRef Astro component

**Files:**
- Create: `www/src/components/ConfigRef.astro`

**Step 1: Create the component**

```astro
---
interface Props {
  keys: Array<{
    key: string;
    type: string;
    default: string;
    description: string;
  }>;
}

const { keys } = Astro.props;
---

<table>
  <thead>
    <tr>
      <th>Key</th>
      <th>Type</th>
      <th>Default</th>
      <th>Description</th>
    </tr>
  </thead>
  <tbody>
    {keys.map(k => (
      <tr>
        <td><code>{k.key}</code></td>
        <td><code>{k.type}</code></td>
        <td>{k.default === '' ? '--' : <code>{k.default}</code>}</td>
        <td>{k.description}</td>
      </tr>
    ))}
  </tbody>
</table>
```

**Step 2: Commit**

```bash
git add www/src/components/ConfigRef.astro
git commit -m "feat: add ConfigRef Astro component"
```

---

### Task 16: Refactor commands.mdx to use generated data

**Files:**
- Modify: `www/src/content/docs/commands.mdx`

**Step 1: Import JSON data and components at top of commands.mdx**

Replace the hand-written flag tables and usage sections with component renders. Keep narrative prose (conflict resolution flow, workflow descriptions) as hand-written MDX.

The structure becomes:
1. Import CLI reference JSON
2. For each command category, render `<CommandRef>` components
3. Keep the narrative sections (like conflict resolution under sync) as hand-written MDX between the generated sections

**Step 2: Verify Astro builds**

Run: `cd www && npm run build`
Expected: build succeeds, pages render correctly

**Step 3: Verify the rendered output**

Run: `cd www && npm run dev` and check http://localhost:4321/docs/commands visually.
Expected: commands page looks the same as before, with flag tables and examples from JSON.

**Step 4: Commit**

```bash
git add www/src/content/docs/commands.mdx
git commit -m "refactor: render command reference from generated JSON"
```

---

### Task 17: Refactor configuration.mdx to use generated data

**Files:**
- Modify: `www/src/content/docs/configuration.mdx`

**Step 1: Replace the hand-written config key table**

Use `<ConfigRef>` component for the "Valid keys" table. Keep the explanatory prose sections for each config group.

**Step 2: Verify Astro builds**

Run: `cd www && npm run build`
Expected: build succeeds

**Step 3: Commit**

```bash
git add www/src/content/docs/configuration.mdx
git commit -m "refactor: render config key reference from generated JSON"
```

---

### Task 18: Final verification

**Files:** None (verification only)

**Step 1: Run full Go test suite**

Run: `go test ./... -count=1`
Expected: all pass

**Step 2: Run docs-check**

Run: `make docs-check`
Expected: "CLI reference is up to date." + validation passes

**Step 3: Run Astro build**

Run: `cd www && npm run build`
Expected: clean build

**Step 4: Verify CI config**

Read `.github/workflows/ci.yml` and confirm the docs-check step is in place.

**Step 5: Install and smoke test**

Run: `go install ./...`
Run: `sdf help`, `sdf sync --help`, `sdf config set --help`
Expected: Cobra-formatted help output

**Step 6: Final commit if any fixups needed**

```bash
git add -A
git commit -m "chore: final cleanup for docs sync implementation"
```

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/pavelpascari/sdf/cmd"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "init":
		exitOnErr(cmd.RunInit(args))
	case "branch":
		exitOnErr(cmd.RunBranch(args))
	case "status":
		exitOnErr(cmd.RunStatus(args))
	case "sync":
		exitOnErr(cmd.RunSync(args))
	case "pr":
		exitOnErr(cmd.RunPR(args))
	case "move":
		exitOnErr(cmd.RunMove(args))
	case "register":
		exitOnErr(cmd.RunRegister(args))
	case "context":
		exitOnErr(cmd.RunContext(args))
	case "switch":
		exitOnErr(cmd.RunSwitch(args))
	case "config":
		exitOnErr(cmd.RunConfig(args))
case "version", "--version", "-v":
		fmt.Printf("sdf %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	case "doctor":
		exitOnErr(runDoctor())
	default:
		// Try as branch shorthand: sdf <branch> → sdf switch <branch>
		if cmd.TrySwitch(command) == nil {
			return
		}
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func exitOnErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`sdf — Stacked Diffs Flow

Usage:
  sdf <command> [arguments]

Stack commands:
  init [--stack] <name>              Initialize a new stack in the current repo
  register                           Discover and register existing PR stacks
  branch [--no-prefix] <name>        Create a new branch in the stack
  status [--stack <name>]            Show stack topology and sync state
  sync [-y] [--continue] [--stack <name>]  Detect merged PRs, cascade rebase, push
move <commit>...                   Move commits from current branch to parent
  pr                                 Create a GitHub PR for the current branch

Navigation:
  switch [<branch>]                  Switch to a branch (shows stack context)
  <branch>                           Shorthand for switch <branch>

Context commands:
  context show              Print assembled context for current branch
  context edit              Open context doc in $EDITOR
  context update            Ask Claude to rewrite context doc

Config commands:
  config show               Display effective (merged) configuration
  config set <key> <value>  Set a config value in repo or --global config

Other:
  doctor                    Check that dependencies are available
  version                   Print version
  help                      Show this help
`)
}

// runDoctor checks that all required dependencies are available.
func runDoctor() error {
	fmt.Println("sdf doctor — checking dependencies")
	fmt.Println()
	allOk := true

	// git (required)
	if path, err := exec.LookPath("git"); err != nil {
		fmt.Println("  ✗ git        not found (required)")
		allOk = false
	} else {
		ver := getVersion("git", "--version")
		fmt.Printf("  ✓ git        %s (%s)\n", ver, path)
	}

	// gh (required for PR operations)
	if path, err := exec.LookPath("gh"); err != nil {
		fmt.Println("  ● gh         not found (needed for PR operations)")
	} else {
		ver := getVersion("gh", "version")
		fmt.Printf("  ✓ gh         %s (%s)\n", ver, path)
	}

	// claude (optional, needed for context update and conflict resolution)
	if path, err := exec.LookPath("claude"); err != nil {
		fmt.Println("  ● claude     not found (needed for context update and conflict resolution)")
	} else {
		ver := getVersion("claude", "--version")
		fmt.Printf("  ✓ claude     %s (%s)\n", ver, path)
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
	// Take first line only
	line := strings.Split(strings.TrimSpace(string(out)), "\n")[0]
	return line
}

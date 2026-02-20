package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	"github.com/pavelpascari/sdf/internal/stack"
)

func RunConfig(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: sdf config <show|set>")
		fmt.Fprintln(os.Stderr, "  show            Display effective (merged) configuration")
		fmt.Fprintln(os.Stderr, "  set <key> <val> Set a value in repo config (.sdf/config.json)")
		os.Exit(1)
	}

	switch args[0] {
	case "show":
		return runConfigShow()
	case "set":
		return runConfigSet(args[1:])
	default:
		return fmt.Errorf("unknown config subcommand: %s", args[0])
	}
}

func runConfigShow() error {
	root, err := stack.FindRoot()
	if err != nil {
		// If not in a stack, try to show just global config
		fmt.Fprintln(os.Stderr, "Not inside an sdf stack — showing global config only.")
		globalPath, err := cfgpkg.GlobalPath()
		if err != nil {
			return err
		}
		cfg, err := cfgpkg.Load("/dev/null") // no repo config
		if err != nil {
			return err
		}
		return printConfig(cfg, globalPath, "")
	}

	cfg, err := cfgpkg.Load(root)
	if err != nil {
		return err
	}

	globalPath, _ := cfgpkg.GlobalPath()
	repoPath := cfgpkg.RepoPath(root)
	return printConfig(cfg, globalPath, repoPath)
}

func printConfig(cfg cfgpkg.Config, globalPath, repoPath string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println("Effective configuration (merged):")
	fmt.Println(string(data))
	fmt.Println()
	fmt.Printf("Global: %s\n", globalPath)
	if repoPath != "" {
		fmt.Printf("Repo:   %s\n", repoPath)
	}
	return nil
}

func runConfigSet(args []string) error {
	fs := flag.NewFlagSet("config set", flag.ExitOnError)
	global := fs.Bool("global", false, "set value in global config (~/.config/sdf/config.json)")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: sdf config set [--global] <key> <value>")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Keys:")
		fmt.Fprintln(os.Stderr, "  branch_prefix.enabled              true or false")
		fmt.Fprintln(os.Stderr, "  branch_prefix.prefix               prefix string (empty = use stack_id)")
		fmt.Fprintln(os.Stderr, "  branch_prefix.separator            separator character (default: /)")
		fmt.Fprintln(os.Stderr, "  pr_title.conventional_commits      true or false")
		fmt.Fprintln(os.Stderr, "  pr_title.ticket_pattern            regex to extract ticket from branch name")
		fmt.Fprintln(os.Stderr, "  sync.update_descriptions           true or false")
		fmt.Fprintln(os.Stderr, "  sync.update_titles                 true or false")
		os.Exit(1)
	}

	key := fs.Arg(0)
	value := fs.Arg(1)

	// Determine which config file to modify
	var path string
	if *global {
		var err error
		path, err = cfgpkg.GlobalPath()
		if err != nil {
			return err
		}
	} else {
		root, err := stack.FindRoot()
		if err != nil {
			return fmt.Errorf("not inside an sdf stack — use --global for global config: %w", err)
		}
		path = cfgpkg.RepoPath(root)
	}

	// Load existing config from that specific file (not merged)
	cfg := cfgpkg.Config{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &cfg)
	}

	// Apply the setting
	switch key {
	case "branch_prefix.enabled":
		val := strings.ToLower(value) == "true"
		cfg.BranchPrefix.Enabled = &val
	case "branch_prefix.prefix":
		cfg.BranchPrefix.Prefix = value
	case "branch_prefix.separator":
		cfg.BranchPrefix.Separator = value
	case "pr_title.conventional_commits":
		val := strings.ToLower(value) == "true"
		cfg.PRTitle.ConventionalCommits = &val
	case "pr_title.ticket_pattern":
		cfg.PRTitle.TicketPattern = value
	case "sync.update_descriptions":
		val := strings.ToLower(value) == "true"
		cfg.Sync.UpdateDescriptions = &val
	case "sync.update_titles":
		val := strings.ToLower(value) == "true"
		cfg.Sync.UpdateTitles = &val
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}

	if err := cfgpkg.Save(path, cfg); err != nil {
		return err
	}

	fmt.Printf("Set %s = %s in %s\n", key, value, path)
	return nil
}

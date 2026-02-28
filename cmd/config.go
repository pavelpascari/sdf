package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
)

var configCmd = &cobra.Command{
	Use:         "config",
	Short:       "Manage sdf configuration",
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

func runConfigShowCmd(cmd *cobra.Command, args []string) error {
	return runConfigShow()
}

func runConfigSetCmd(cmd *cobra.Command, args []string) error {
	global, _ := cmd.Flags().GetBool("global")
	key := args[0]
	value := args[1]

	// Determine which config file to modify
	var path string
	if global {
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
	case "branch_prefix.scope":
		cfg.BranchPrefix.Scope = value
	case "branch_prefix.separator":
		cfg.BranchPrefix.Separator = value
	case "pr_title.conventional_commits":
		val := strings.ToLower(value) == "true"
		cfg.PRTitle.ConventionalCommits = &val
	case "pr_title.ticket_pattern":
		cfg.PRTitle.TicketPattern = value
	case "sync.with_content":
		val := strings.ToLower(value) == "true"
		cfg.Sync.WithContent = &val
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}

	if err := cfgpkg.Save(path, cfg); err != nil {
		return err
	}

	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
	defer func() { _ = bus.Finish() }()
	bus.Printf("Set %s = %s in %s", key, value, path)
	return nil
}

// RunConfig is a compatibility wrapper for callers that use the old interface.
func RunConfig(args []string) error {
	rootCmd.SetArgs(append([]string{"config"}, args...))
	return rootCmd.Execute()
}

func runConfigShow() error {
	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
	defer func() { _ = bus.Finish() }()

	root, err := stack.FindRoot()
	if err != nil {
		// If not in a stack, try to show just global config
		bus.Warnf("Not inside an sdf stack — showing global config only.")
		globalPath, err := cfgpkg.GlobalPath()
		if err != nil {
			return err
		}
		cfg, err := cfgpkg.Load("/dev/null") // no repo config
		if err != nil {
			return err
		}
		return printConfig(cfg, globalPath, "", bus)
	}

	cfg, err := cfgpkg.Load(root)
	if err != nil {
		return err
	}

	globalPath, _ := cfgpkg.GlobalPath()
	repoPath := cfgpkg.RepoPath(root)
	return printConfig(cfg, globalPath, repoPath, bus)
}

func printConfig(cfg cfgpkg.Config, globalPath, repoPath string, bus *render.Bus) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	bus.Print("Effective configuration (merged):")
	bus.Print(string(data))
	bus.Print("")
	bus.Printf("Global: %s", globalPath)
	if repoPath != "" {
		bus.Printf("Repo:   %s", repoPath)
	}
	return nil
}

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

// CLIReference is the top-level JSON output structure.
type CLIReference struct {
	Generated  string                 `json:"generated,omitempty"`
	Commands   []CommandDoc           `json:"commands"`
	ConfigKeys []config.ConfigKeyMeta `json:"config_keys"`
}

// CommandDoc describes a single CLI command.
type CommandDoc struct {
	Name        string       `json:"name"`
	Category    string       `json:"category,omitempty"`
	Use         string       `json:"use"`
	Short       string       `json:"short"`
	Long        string       `json:"long,omitempty"`
	Example     string       `json:"example,omitempty"`
	Deprecated  string       `json:"deprecated,omitempty"`
	Flags       []FlagDoc    `json:"flags,omitempty"`
	Subcommands []CommandDoc `json:"subcommands,omitempty"`
}

// FlagDoc describes a single command flag.
type FlagDoc struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

func main() {
	noTimestamp := false
	for _, arg := range os.Args[1:] {
		if arg == "--no-timestamp" {
			noTimestamp = true
		}
	}

	root := cmd.RootCmd()

	ref := CLIReference{
		Commands:   extractCommands(root),
		ConfigKeys: config.ConfigKeys(),
	}
	if !noTimestamp {
		ref.Generated = time.Now().UTC().Format(time.RFC3339)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ref); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func extractCommands(parent *cobra.Command) []CommandDoc {
	var docs []CommandDoc
	for _, c := range parent.Commands() {
		if c.Hidden || c.Name() == "help" {
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
			if f.Hidden || f.Name == "help" {
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

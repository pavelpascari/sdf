package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
	"github.com/spf13/cobra"
)

type LSResult struct {
	Stacks []LSStack `json:"stacks"`
}

type LSStack struct {
	Name      string `json:"name"`
	Nodes     int    `json:"nodes"`
	Merged    int    `json:"merged"`
	Status    string `json:"status"`
	IsCurrent bool   `json:"is_current,omitempty"`
	Worktree  bool   `json:"worktree,omitempty"`
}

var lsCmd = &cobra.Command{
	Use:         "ls",
	Short:       "List local sdf stacks and their statuses",
	Annotations: map[string]string{"category": "stack"},
	RunE:        runLS,
}

func init() {
	rootCmd.AddCommand(lsCmd)
	lsCmd.Flags().Bool("json", false, "output result as JSON")
}

func runLS(cmd *cobra.Command, args []string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	var rdr render.Renderer
	if jsonFlag {
		rdr = &render.JSONRenderer{}
	}
	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{Renderer: rdr})
	if !jsonFlag {
		defer func() { _ = bus.Finish() }()
	}

	stacks, err := stack.LoadAll(root)
	if err != nil {
		return err
	}

	currentBranch, _ := gitpkg.CurrentBranch()
	currentStackID := ""
	for _, s := range stacks {
		if s.FindNode(currentBranch) != nil {
			currentStackID = s.StackID
			break
		}
	}

	result := LSResult{Stacks: make([]LSStack, 0, len(stacks))}
	for _, s := range stacks {
		status, merged := summarizeStackStatus(s)
		result.Stacks = append(result.Stacks, LSStack{
			Name:      s.StackID,
			Nodes:     len(s.Nodes),
			Merged:    merged,
			Status:    status,
			IsCurrent: s.StackID == currentStackID,
			Worktree:  s.Worktree,
		})
	}

	if jsonFlag {
		_ = bus.Finish()
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("cannot marshal result: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	if len(result.Stacks) == 0 {
		bus.Print("No stacks found.")
		return nil
	}

	for _, st := range result.Stacks {
		marker := " "
		if st.IsCurrent {
			marker = ui.Cyan.Render("*")
		}

		status := st.Status
		if st.Status == "partial" {
			status = fmt.Sprintf("partial (%d/%d merged)", st.Merged, st.Nodes)
		}
		tag := ""
		if st.Worktree {
			tag = "  " + ui.Cyan.Render("[worktree]")
		}
		bus.Printf("%s  %-20s %d PRs   %s%s", marker, st.Name, st.Nodes, status, tag)
	}
	return nil
}

func summarizeStackStatus(s *stack.Stack) (status string, merged int) {
	if len(s.Nodes) == 0 {
		return "active", 0
	}
	for _, n := range s.Nodes {
		if n.Status == "merged" || n.Status == "closed" {
			merged++
		}
	}
	switch {
	case merged == 0:
		return "active", 0
	case merged == len(s.Nodes):
		return "completed", merged
	default:
		return "partial", merged
	}
}

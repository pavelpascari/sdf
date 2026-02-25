// Package ai provides prompt templates for AI assistant integration.
package ai

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var promptHeader = `You are being asked to set up a Claude Code skill so that you remember how to use SDF (Stacked Diffs Flow) in future sessions in this repository.

## What SDF Does

SDF manages stacked PRs — chains of dependent pull requests where each PR builds on the previous one. It handles branch topology, cascade rebasing when PRs merge, and keeps PR bases correct on GitHub.

## Rules — IMPORTANT

When working in a repository that uses SDF, follow these rules for any branch that is part of an SDF stack:

- Use ` + "`sdf branch <name>`" + ` instead of ` + "`git checkout -b`" + ` — it registers the branch in the stack and sets the correct base
- Use ` + "`sdf sync`" + ` instead of ` + "`git rebase`" + ` — it cascade-rebases the entire stack and updates PR bases
- Use ` + "`sdf pr`" + ` instead of ` + "`gh pr create`" + ` — it sets the correct PR base branch and adds navigation links
- Use ` + "`sdf merge`" + ` instead of ` + "`gh pr merge`" + ` — it retargets the next PR and syncs the remaining stack
- After amending an earlier branch, run ` + "`sdf sync`" + ` to cascade changes forward

## Commands

`

var promptFooter = `
## When to Run What

- **Starting a session:** Run ` + "`sdf ls`" + ` to see all tracked stacks at a glance, then ` + "`sdf status`" + ` on the relevant stack for details.
- **Before creating a branch:** Run ` + "`sdf status`" + ` to confirm you're on the right branch (new branch inserts after current position).
- **After pushing changes to an earlier branch:** Run ` + "`sdf sync`" + ` to cascade.
- **After a PR is merged on GitHub:** Run ` + "`sdf sync`" + ` to rebase remaining branches onto the new base.

## Your Task

Create a Claude Code skill file at ` + "`.claude/skills/sdf/SKILL.md`" + ` that captures all of the above — rules, commands, and "when to run what" — so you remember how to work with SDF in every future session in this repo. Use whatever skill format and frontmatter Claude Code currently expects.`

// BuildIntroPrompt returns the intro prompt that teaches Claude about SDF and
// asks it to create a skill file. The command table is generated dynamically
// from the cobra command tree so it stays current as commands evolve.
func BuildIntroPrompt(root *cobra.Command) string {
	return promptHeader + buildCommandTable(root) + promptFooter
}

// buildCommandTable walks the cobra command tree and produces a markdown table.
func buildCommandTable(root *cobra.Command) string {
	var b strings.Builder
	b.WriteString("| Command | Purpose |\n")
	b.WriteString("|---------|---------|")
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		// Skip the ai command itself — it's meta, not part of the workflow.
		if c.Name() == "ai" || c.Name() == "version" {
			continue
		}
		fmt.Fprintf(&b, "\n| `sdf %s` | %s |", c.Use, c.Short)
	}
	return b.String()
}

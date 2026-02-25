// Package ai provides prompt templates for AI assistant integration.
package ai

const bt = "`"

var introPrompt = `You are being asked to set up a Claude Code skill so that you remember how to use SDF (Stacked Diffs Flow) in future sessions in this repository.

## What SDF Does

SDF manages stacked PRs — chains of dependent pull requests where each PR builds on the previous one. It handles branch topology, cascade rebasing when PRs merge, and keeps PR bases correct on GitHub.

## Rules — IMPORTANT

When working in a repository that uses SDF, follow these rules for any branch that is part of an SDF stack:

- Use ` + bt + `sdf branch <name>` + bt + ` instead of ` + bt + `git checkout -b` + bt + ` — it registers the branch in the stack and sets the correct base
- Use ` + bt + `sdf sync` + bt + ` instead of ` + bt + `git rebase` + bt + ` — it cascade-rebases the entire stack and updates PR bases
- Use ` + bt + `sdf pr` + bt + ` instead of ` + bt + `gh pr create` + bt + ` — it sets the correct PR base branch and adds navigation links
- Use ` + bt + `sdf merge` + bt + ` instead of ` + bt + `gh pr merge` + bt + ` — it retargets the next PR and syncs the remaining stack
- After amending an earlier branch, run ` + bt + `sdf sync` + bt + ` to cascade changes forward

## Commands

| Command | Purpose |
|---------|---------|
| ` + bt + `sdf init <name> [--branch <name>]` + bt + ` | Create a new stack with its first branch |
| ` + bt + `sdf branch <name>` + bt + ` | Add a branch after the current position in the stack |
| ` + bt + `sdf ls` + bt + ` | List all tracked stacks with a short summary of each |
| ` + bt + `sdf status [<stack>]` + bt + ` | Show detailed stack topology, PR states, sync status |
| ` + bt + `sdf sync [--with-content]` + bt + ` | Cascade-rebase after merges or amendments |
| ` + bt + `sdf pr [--title "..."]` + bt + ` | Create a GitHub PR with correct base and links |
| ` + bt + `sdf merge [-y] [--method squash|merge|rebase]` + bt + ` | Merge head PR and sync |
| ` + bt + `sdf switch [<branch>]` + bt + ` | Checkout a branch and show its stack context |
| ` + bt + `sdf move <commit>...` + bt + ` | Move commits from current branch to parent |
| ` + bt + `sdf fetch` + bt + ` | Discover existing PR chains from GitHub |
| ` + bt + `sdf doctor` + bt + ` | Check that git, gh, and claude are available |

## When to Run What

- **Starting a session:** Run ` + bt + `sdf ls` + bt + ` to see all tracked stacks at a glance, then ` + bt + `sdf status` + bt + ` on the relevant stack for details.
- **Before creating a branch:** Run ` + bt + `sdf status` + bt + ` to confirm you're on the right branch (new branch inserts after current position).
- **After pushing changes to an earlier branch:** Run ` + bt + `sdf sync` + bt + ` to cascade.
- **After a PR is merged on GitHub:** Run ` + bt + `sdf sync` + bt + ` to rebase remaining branches onto the new base.

## Your Task

Create a Claude Code skill file at ` + bt + `.claude/skills/sdf/SKILL.md` + bt + ` that captures all of the above — rules, commands, and "when to run what" — so you remember how to work with SDF in every future session in this repo. Use whatever skill format and frontmatter Claude Code currently expects.`

// BuildIntroPrompt returns the intro prompt that teaches Claude about SDF and
// asks it to create a skill file.
func BuildIntroPrompt() string {
	return introPrompt
}

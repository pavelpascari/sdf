package config

import (
	"regexp"
	"strings"
)

// conventionalTypes are the recognized conventional commit prefixes.
var conventionalTypes = []string{
	"feat", "fix", "docs", "style", "refactor",
	"perf", "test", "build", "ci", "chore", "revert",
}

// GeneratePRTitle builds a PR title from the branch name and commit messages,
// applying config settings for conventional commits and ticket extraction.
//
// When conventional commits is disabled, returns the humanized branch name
// (prefix stripped, dashes/underscores → spaces).
//
// When enabled, returns "type(ticket): description" or "type: description".
func GeneratePRTitle(cfg Config, stackID, branch string, commitSubjects []string) string {
	if !cfg.ConventionalCommitsEnabled() {
		return humanizeBranch(cfg, stackID, branch)
	}

	commitType := detectCommitType(commitSubjects)
	ticket := extractTicket(cfg, branch)
	desc := humanizeBranch(cfg, stackID, stripTicket(cfg, branch))

	if ticket != "" {
		return commitType + "(" + ticket + "): " + desc
	}
	return commitType + ": " + desc
}

// TitlePrefix returns the conventional commit prefix for a title
// (e.g. "feat: ", "fix(PROJ-123): "). Returns empty string if
// conventional commits are disabled.
func TitlePrefix(cfg Config, branch string, commitSubjects []string) string {
	if !cfg.ConventionalCommitsEnabled() {
		return ""
	}

	commitType := detectCommitType(commitSubjects)
	ticket := extractTicket(cfg, branch)

	if ticket != "" {
		return commitType + "(" + ticket + "): "
	}
	return commitType + ": "
}

// humanizeBranch strips the configured prefix and converts the remaining
// branch name into a human-readable title.
func humanizeBranch(cfg Config, stackID, branch string) string {
	name := StripPrefix(cfg, stackID, branch)
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	// Collapse multiple spaces and trim
	for strings.Contains(name, "  ") {
		name = strings.ReplaceAll(name, "  ", " ")
	}
	return strings.TrimSpace(name)
}

// detectCommitType scans commit subjects for conventional commit prefixes
// and returns the most common one. Defaults to "feat" if none found.
func detectCommitType(subjects []string) string {
	counts := make(map[string]int)

	for _, subj := range subjects {
		subj = strings.TrimSpace(subj)
		for _, t := range conventionalTypes {
			// Match "type:" or "type(scope):"
			if strings.HasPrefix(subj, t+":") || strings.HasPrefix(subj, t+"(") {
				counts[t]++
				break
			}
		}
	}

	if len(counts) == 0 {
		return "feat"
	}

	best := ""
	bestCount := 0
	for t, c := range counts {
		if c > bestCount {
			best = t
			bestCount = c
		}
	}
	return best
}

// stripTicket removes the ticket match and surrounding separators from the branch name.
func stripTicket(cfg Config, branch string) string {
	pattern := cfg.PRTitle.TicketPattern
	if pattern == "" {
		return branch
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return branch
	}

	result := re.ReplaceAllString(branch, "")
	// Clean up leftover double-separators
	result = strings.ReplaceAll(result, "--", "-")
	result = strings.TrimLeft(result, "-")
	result = strings.TrimRight(result, "-")
	return result
}

// extractTicket applies the configured ticket_pattern regex to the branch name
// and returns the first match. Returns empty string if no pattern or no match.
func extractTicket(cfg Config, branch string) string {
	pattern := cfg.PRTitle.TicketPattern
	if pattern == "" {
		return ""
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}

	matches := re.FindStringSubmatch(branch)
	if len(matches) >= 2 {
		return matches[1] // first capture group
	}
	if len(matches) == 1 {
		return matches[0] // whole match if no capture group
	}
	return ""
}

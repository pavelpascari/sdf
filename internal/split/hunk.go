package split

import (
	"fmt"
	"strings"
)

// Hunk represents a single diff hunk within a file.
type Hunk struct {
	Header string // the @@ line (with trailing newline)
	Body   string // lines after the header, up to next hunk or file
}

// FileDiff represents all hunks for a single file in a unified diff.
type FileDiff struct {
	Header string // diff --git, index, ---, +++ lines (with newlines)
	Path   string // file path extracted from diff --git header
	Hunks  []Hunk
}

// ParseDiff splits a unified diff string into per-file sections with numbered hunks.
func ParseDiff(diff string) []FileDiff {
	if diff == "" {
		return nil
	}

	lines := strings.Split(diff, "\n")
	// Remove trailing empty string from Split if diff ends with newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var result []FileDiff
	var current *FileDiff
	var currentHunk *Hunk

	flush := func() {
		if currentHunk != nil && current != nil {
			current.Hunks = append(current.Hunks, *currentHunk)
			currentHunk = nil
		}
		if current != nil {
			result = append(result, *current)
			current = nil
		}
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			current = &FileDiff{Header: line + "\n"}
			parts := strings.SplitN(line, " ", 4)
			if len(parts) >= 3 {
				current.Path = strings.TrimPrefix(parts[2], "a/")
			}
			continue
		}

		if strings.HasPrefix(line, "@@") && current != nil {
			if currentHunk != nil {
				current.Hunks = append(current.Hunks, *currentHunk)
			}
			currentHunk = &Hunk{Header: line + "\n"}
			continue
		}

		if currentHunk != nil {
			currentHunk.Body += line + "\n"
		} else if current != nil {
			current.Header += line + "\n"
		}
	}

	flush()
	return result
}

// FilterHunks reconstructs a valid patch containing only the specified hunks
// from a FileDiff. The returned string is suitable for git apply.
func FilterHunks(fd FileDiff, indices []int) string {
	var b strings.Builder
	b.WriteString(fd.Header)
	for _, idx := range indices {
		if idx >= 0 && idx < len(fd.Hunks) {
			b.WriteString(fd.Hunks[idx].Header)
			b.WriteString(fd.Hunks[idx].Body)
		}
	}
	return b.String()
}

// FormatNumberedHunks formats a file's hunks with numbered labels,
// suitable for including in the hunk assignment prompt.
func FormatNumberedHunks(fd FileDiff) string {
	var b strings.Builder
	for i, h := range fd.Hunks {
		fmt.Fprintf(&b, "Hunk %d:\n", i)
		b.WriteString(h.Header)
		b.WriteString(h.Body)
	}
	return b.String()
}

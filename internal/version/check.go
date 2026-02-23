// Package version checks for newer SDF releases by querying
// GitHub via the gh CLI.
package version

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/ui"
)

// LatestReleaseFunc is the function used to fetch the latest release.
// Tests can replace it to avoid shelling out to gh.
var LatestReleaseFunc = gh.LatestRelease

// Check queries GitHub for the latest release and prints an upgrade
// notice if the running version is older.
// Errors are silently ignored — the version check must never block or
// break normal CLI usage.
func Check(current string) {
	if current == "" || current == "dev" {
		return
	}

	rel, err := LatestReleaseFunc()
	if err != nil {
		return
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	if latest == "" {
		return
	}

	if !isNewer(latest, current) {
		return
	}

	printUpgradeNotice(current, latest, rel.URL)
}

// isNewer returns true if latest is a higher semver than current.
// Both strings should be bare versions like "0.2.0" (no "v" prefix).
func isNewer(latest, current string) bool {
	lMajor, lMinor, lPatch, lok := parseSemver(latest)
	cMajor, cMinor, cPatch, cok := parseSemver(current)
	if !lok || !cok {
		return false
	}

	if lMajor != cMajor {
		return lMajor > cMajor
	}
	if lMinor != cMinor {
		return lMinor > cMinor
	}
	return lPatch > cPatch
}

// parseSemver extracts major, minor, patch from a version string.
// Accepts "1.2.3" or "v1.2.3". Returns ok=false on parse failure.
func parseSemver(v string) (major, minor, patch int, ok bool) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return 0, 0, 0, false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, false
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, false
	}
	// Handle pre-release suffixes like "1.2.3-beta" by taking only digits.
	patchStr := parts[2]
	if idx := strings.IndexFunc(patchStr, func(r rune) bool { return r < '0' || r > '9' }); idx > 0 {
		patchStr = patchStr[:idx]
	}
	patch, err = strconv.Atoi(patchStr)
	if err != nil {
		return 0, 0, 0, false
	}

	return major, minor, patch, true
}

func printUpgradeNotice(current, latest, releaseURL string) {
	fmt.Println()
	fmt.Printf("%s A new version of sdf is available: %s → %s\n",
		ui.SymWarn,
		ui.Gray.Render(current),
		ui.Green.Render(latest),
	)
	fmt.Printf("  Upgrade: %s\n", ui.Bold.Render("go install github.com/pavelpascari/sdf@latest"))
	if releaseURL != "" {
		fmt.Printf("  Release: %s\n", ui.Cyan.Render(releaseURL))
	}
}

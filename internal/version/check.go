// Package version checks for newer SDF releases by querying the
// version endpoint on sdf-tool.com.
package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pavelpascari/sdf/internal/ui"
)

// VersionURL is the endpoint that returns the latest release information.
// It is a variable so tests can override it.
var VersionURL = "https://sdf-tool.com/_/version.json"

// HTTPClient is the HTTP client used for the version check.
// Tests can replace it to avoid real network calls.
var HTTPClient = &http.Client{Timeout: 3 * time.Second}

// response is the JSON shape returned by the version endpoint.
type response struct {
	Version   string `json:"version"`
	Changelog string `json:"changelog"`
}

// Check fetches the latest version from the remote endpoint and prints
// an upgrade notice to stdout if the running version is older.
// Errors are silently ignored — the version check must never block or
// break normal CLI usage.
func Check(current string) {
	if current == "" || current == "dev" {
		return
	}

	resp, err := HTTPClient.Get(VersionURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var info response
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return
	}

	if info.Version == "" {
		return
	}

	if !isNewer(info.Version, current) {
		return
	}

	printUpgradeNotice(current, info.Version, info.Changelog)
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

func printUpgradeNotice(current, latest, changelog string) {
	fmt.Println()
	fmt.Printf("%s A new version of sdf is available: %s → %s\n",
		ui.SymWarn,
		ui.Gray.Render(current),
		ui.Green.Render(latest),
	)
	fmt.Printf("  Upgrade: %s\n", ui.Bold.Render("go install github.com/pavelpascari/sdf@latest"))
	if changelog != "" {
		fmt.Printf("  Changelog: %s\n", ui.Cyan.Render(changelog))
	}
}

//go:build e2e

// Package e2e contains end-to-end tests that exercise sdf against a real
// GitHub repository. These tests create real branches, real PRs, and
// perform real git/gh operations.
//
// Prerequisites:
//   - GH_TOKEN or GITHUB_TOKEN set with repo scope
//   - SDF_E2E_REPO set to the path of a cloned test sandbox repo
//     (e.g., /tmp/sandbox from `gh repo clone owner/sdf-test-sandbox`)
//   - sdf binary built and on PATH (or SDF_BIN set to the binary path)
//
// Run with:
//
//	go test -tags e2e -v -count=1 ./e2e/...
//
// To also run Claude-dependent tests:
//
//	go test -tags e2e -v -count=1 -run TestE2E ./e2e/... -args -with-claude
package e2e

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

var withClaude = flag.Bool("with-claude", false, "include tests that call the Claude API")

// testPrefix generates a unique prefix for branches created by this test run
// to avoid collisions with other test runs or manual work.
func testPrefix() string {
	return fmt.Sprintf("e2e-%d-%04x", time.Now().Unix(), rand.Intn(0xFFFF))
}

// e2eRepo returns the path to the E2E test sandbox repo.
// Skips the test if SDF_E2E_REPO is not set.
func e2eRepo(t *testing.T) string {
	t.Helper()
	repo := os.Getenv("SDF_E2E_REPO")
	if repo == "" {
		t.Skip("SDF_E2E_REPO not set — skipping E2E test")
	}
	return repo
}

// sdfBin returns the path to the sdf binary.
func sdfBin(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("SDF_BIN"); bin != "" {
		return bin
	}
	path, err := exec.LookPath("sdf")
	if err != nil {
		t.Skip("sdf binary not found on PATH and SDF_BIN not set — skipping E2E test")
	}
	return path
}

// runSDF executes an sdf command in the given directory and returns stdout.
func runSDF(t *testing.T, dir string, args ...string) string {
	t.Helper()
	bin := sdfBin(t)
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		t.Fatalf("sdf %s failed:\n%s", strings.Join(args, " "), output)
	}
	return output
}

// runSdfMayFail executes sdf and returns output + error (does not fail the test).
func runSdfMayFail(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	bin := sdfBin(t)
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// runGit executes a git command in the given directory.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		t.Fatalf("git %s failed:\n%s", strings.Join(args, " "), output)
	}
	return output
}

// runGH executes a gh command in the given directory.
func runGH(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		t.Fatalf("gh %s failed:\n%s", strings.Join(args, " "), output)
	}
	return output
}

// writeCommit creates a file, adds, and commits it.
func writeCommit(t *testing.T, dir, filename, content, message string) {
	t.Helper()
	if err := os.WriteFile(dir+"/"+filename, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", filename)
	runGit(t, dir, "commit", "-m", message)
}

// cleanupBranches deletes remote branches matching a prefix.
func cleanupBranches(t *testing.T, dir, prefix string) {
	t.Helper()
	out := runGit(t, dir, "branch", "-r", "--list", fmt.Sprintf("origin/%s*", prefix))
	if out == "" {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		branch := strings.TrimSpace(strings.TrimPrefix(line, "origin/"))
		if branch == "" {
			continue
		}
		cmd := exec.Command("git", "push", "origin", "--delete", branch)
		cmd.Dir = dir
		cmd.CombinedOutput() // best-effort cleanup
	}
}

// cleanupPRs closes any open PRs whose head branch matches the prefix.
func cleanupPRs(t *testing.T, dir, prefix string) {
	t.Helper()
	cmd := exec.Command("gh", "pr", "list", "--state", "open",
		"--json", "number,headRefName", "--limit", "100")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return // best-effort
	}

	// Quick and dirty JSON parse — just find PR numbers matching prefix
	output := string(out)
	if !strings.Contains(output, prefix) {
		return
	}

	// Close matching PRs
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, prefix) {
			// Extract number — find "number":N
			if idx := strings.Index(line, `"number":`); idx >= 0 {
				numStr := line[idx+9:]
				if end := strings.IndexAny(numStr, ",}"); end >= 0 {
					num := strings.TrimSpace(numStr[:end])
					cmd := exec.Command("gh", "pr", "close", num)
					cmd.Dir = dir
					cmd.CombinedOutput() // best-effort
				}
			}
		}
	}
}

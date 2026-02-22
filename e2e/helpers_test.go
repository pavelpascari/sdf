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
	"encoding/json"
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

// TestMain runs global setup/teardown for the E2E suite.
// After all tests complete (pass or fail), it sweeps stale e2e-*
// branches and PRs from the sandbox repo — catching orphans from
// crashed or timed-out runs.
func TestMain(m *testing.M) {
	flag.Parse()
	code := m.Run()

	if repo := os.Getenv("SDF_E2E_REPO"); repo != "" {
		sweepStaleResources(repo)
	}

	os.Exit(code)
}

// sweepStaleResources removes any e2e-* branches and PRs left in the
// sandbox repo. Best-effort — failures are logged but don't affect
// the exit code.
func sweepStaleResources(repo string) {
	fmt.Println("\n--- E2E cleanup: sweeping stale resources ---")

	// Fetch so we see all remote branches
	run := func(name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	run("git", "fetch", "--prune")

	// Close open PRs with e2e- prefix
	if out, err := run("gh", "pr", "list", "--state", "open",
		"--json", "number,headRefName", "--limit", "200"); err == nil {
		var prs []struct {
			Number      int    `json:"number"`
			HeadRefName string `json:"headRefName"`
		}
		if json.Unmarshal([]byte(out), &prs) == nil {
			for _, pr := range prs {
				if strings.HasPrefix(pr.HeadRefName, "e2e-") {
					fmt.Printf("  closing PR #%d (%s)\n", pr.Number, pr.HeadRefName)
					run("gh", "pr", "close", fmt.Sprint(pr.Number))
				}
			}
		}
	}

	// Delete remote branches with e2e- prefix
	if out, err := run("git", "branch", "-r", "--list", "origin/e2e-*"); err == nil && out != "" {
		for _, line := range strings.Split(out, "\n") {
			branch := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "origin/"))
			if branch == "" {
				continue
			}
			fmt.Printf("  deleting branch %s\n", branch)
			run("git", "push", "origin", "--delete", branch)
		}
	}

	fmt.Println("--- E2E cleanup complete ---")
}

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

// runGitMayFail executes a git command and returns output + error (does not fail the test).
func runGitMayFail(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
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
	runGitMayFail(t, dir, "fetch", "--prune")
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

	var prs []struct {
		Number      int    `json:"number"`
		HeadRefName string `json:"headRefName"`
	}
	if json.Unmarshal(out, &prs) != nil {
		return
	}
	for _, pr := range prs {
		if strings.HasPrefix(pr.HeadRefName, prefix) {
			cmd := exec.Command("gh", "pr", "close", fmt.Sprint(pr.Number))
			cmd.Dir = dir
			cmd.CombinedOutput() // best-effort
		}
	}
}

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
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pavelpascari/sdf/internal/spy"
)

var withClaude = flag.Bool("with-claude", false, "include tests that call the Claude API")

// runID identifies this test run. Set once in TestMain.
// Format: "2006-01-02T15-04-05Z-<random>" (human-readable timestamp + uniquifier).
var runID string

// Per-test recorders, keyed by t.Name().
var sdfSpies sync.Map  // sdf CLI invocations
var gitSpies sync.Map  // git CLI invocations from tests (setup/modeling user behavior)
var ghSpies sync.Map   // gh CLI invocations from tests (verification/modeling user behavior)
var fullSpies sync.Map // combined log of all invocations

// recordingsBaseDir returns the path to the recordings root directory.
func recordingsBaseDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata", "recordings")
}

// setupRecording creates a per-test recording directory at
// e2e/testdata/recordings/<runID>/<testName>/ and configures spy
// recording for sdf, git (via per-test recorders), gh/claude
// (via SDF_SPY_DIR env var inherited by child processes), and
// a combined full.jsonl log of all invocations in order.
func setupRecording(t *testing.T) {
	t.Helper()
	testDir := filepath.Join(recordingsBaseDir(), runID, t.Name())
	os.MkdirAll(testDir, 0755)

	// gh/claude recording: child sdf processes read this env var in init()
	t.Setenv("SDF_SPY_DIR", testDir)

	// Per-tool recorders + combined full log
	sdfRec := spy.NewRecorderFor(testDir, "testing", "sdf")
	gitRec := spy.NewRecorderFor(testDir, "testing", "git")
	ghRec := spy.NewRecorderFor(testDir, "testing", "gh")
	fullRec := spy.NewRecorder(testDir, "full")

	sdfSpies.Store(t.Name(), sdfRec)
	gitSpies.Store(t.Name(), gitRec)
	ghSpies.Store(t.Name(), ghRec)
	fullSpies.Store(t.Name(), fullRec)

	t.Cleanup(func() {
		sdfRec.Close()
		gitRec.Close()
		ghRec.Close()
		fullRec.Close()
		sdfSpies.Delete(t.Name())
		gitSpies.Delete(t.Name())
		ghSpies.Delete(t.Name())
		fullSpies.Delete(t.Name())
	})
}

// spyFor returns the per-test recorder for the given tool. Nil-safe fallback.
func spyFor(m *sync.Map, t *testing.T) *spy.Recorder {
	if v, ok := m.Load(t.Name()); ok {
		return v.(*spy.Recorder)
	}
	return nil
}

// recordInvocation records to both the per-tool spy and the full combined log.
// The actor and binary from the per-tool recorder are forwarded to the full log.
func recordInvocation(t *testing.T, toolSpies *sync.Map, args []string, output string, exitCode int) {
	toolRec := spyFor(toolSpies, t)
	toolRec.Record(args, output, exitCode)
	if fullRec := spyFor(&fullSpies, t); fullRec != nil && toolRec != nil {
		fullRec.RecordAs(toolRec.Name(), toolRec.Binary(), args, output, exitCode)
	}
}

// TestMain runs global setup/teardown for the E2E suite.
// After all tests complete (pass or fail), it sweeps stale e2e-*
// branches and PRs from the sandbox repo — catching orphans from
// crashed or timed-out runs.
func TestMain(m *testing.M) {
	flag.Parse()

	// Generate a human-readable run ID for organizing recordings.
	runID = fmt.Sprintf("%s-%04x", time.Now().UTC().Format("2006-01-02T15-04-05Z"), rand.Intn(0xFFFF))

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
	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	recordInvocation(t, &sdfSpies, args, output, exitCode)
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
	output := strings.TrimSpace(string(out))
	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	recordInvocation(t, &sdfSpies, args, output, exitCode)
	return output, err
}

// runGitMayFail executes a git command and returns output + error (does not fail the test).
func runGitMayFail(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	recordInvocation(t, &gitSpies, args, output, exitCode)
	return output, err
}

// runGit executes a git command in the given directory.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	recordInvocation(t, &gitSpies, args, output, exitCode)
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
	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	recordInvocation(t, &ghSpies, args, output, exitCode)
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

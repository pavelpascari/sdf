//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestE2E_SyncAfterBaseAdvances verifies that when new commits land on main
// (e.g., another PR merged), `sdf sync` correctly cascade-rebases the
// entire stack onto the new main tip.
//
// Scenario: A developer builds a notification system in three stacked PRs:
//
//	main ← add-notification-model ← add-notification-service ← add-notification-ui
//
// Meanwhile, a teammate merges a hotfix to main. After `sdf sync -y`:
//   - All three branches should be rebased onto the new main
//   - All three PRs should remain OPEN
//   - The hotfix commit from main should be reachable from all branches
func TestE2E_SyncAfterBaseAdvances(t *testing.T) {
	dir := e2eRepo(t)
	setupRecording(t)
	prefix := testPrefix()

	t.Cleanup(func() {
		runGit(t, dir, "checkout", "main")
		cleanupPRs(t, dir, prefix)
		cleanupBranches(t, dir, prefix)
		os.RemoveAll(dir + "/.sdf")
	})

	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "pull", "origin", "main")

	stackName := prefix

	// Build a 3-branch notification feature stack
	t.Log("Building notification stack: add-notification-model → add-notification-service → add-notification-ui")

	runSDF(t, dir, "init", "--base", "main", "--branch", "add-notification-model", stackName)
	writeCommit(t, dir, prefix+"-notification.go", "package model\n\ntype Notification struct {\n\tUserID  string\n\tMessage string\n}\n", "feat: add notification data model")

	runSDF(t, dir, "branch", "add-notification-service")
	writeCommit(t, dir, prefix+"-notify-svc.go", "package service\n\nfunc Send(userID, message string) error { return nil }\n", "feat: add notification delivery service")

	runSDF(t, dir, "branch", "add-notification-ui")
	writeCommit(t, dir, prefix+"-notify-ui.tsx", "export const NotificationBell = () => <span className=\"bell\">🔔</span>;\n", "feat: add notification bell component")

	branchModel := stackName + "/add-notification-model"
	branchService := stackName + "/add-notification-service"
	branchUI := stackName + "/add-notification-ui"

	// Create PRs for all branches
	for _, br := range []string{branchModel, branchService, branchUI} {
		runGit(t, dir, "checkout", br)
		out := runSDF(t, dir, "pr", "--json")
		t.Logf("PR created for %s: %s", br, out)
	}

	// Verify: 3 nodes with PRs, all open
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/add-notification-model", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-notification-service", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-notification-ui", HasPR: true, Status: "open"},
	})

	// Simulate a teammate merging an unrelated hotfix to main
	t.Log("Simulating a teammate's hotfix landing on main")
	runGit(t, dir, "checkout", "main")

	mainFile := prefix + "-hotfix.txt"
	writeCommit(t, dir, mainFile, "Fix: corrected timezone handling in cron scheduler.\n", "fix: correct timezone handling in cron scheduler")
	runGit(t, dir, "push", "origin", "main")

	// Record the new main tip for later ancestry verification
	newMainTip := runGit(t, dir, "rev-parse", "HEAD")
	t.Logf("New main tip after hotfix: %s", newMainTip[:12])

	// Sync should cascade-rebase all three branches onto the updated main
	t.Log("Running sdf sync to rebase the notification stack onto updated main")
	runGit(t, dir, "checkout", branchUI)
	syncOut := runSDF(t, dir, "sync", "-y")
	t.Log(syncOut)

	// Sync should have done real work (not "in sync")
	if strings.Contains(syncOut, "Everything is in sync") {
		t.Error("expected sync to detect stale branches, but got 'Everything is in sync'")
	}

	// Should mention rebasing
	if !strings.Contains(syncOut, "rebase") && !strings.Contains(syncOut, "Sync complete") {
		t.Errorf("expected sync to mention rebasing or completion, got: %s", syncOut)
	}

	// Verify: all PRs still open after rebase
	t.Log("Verifying all notification PRs are still open after rebase")
	for _, br := range []string{branchModel, branchService, branchUI} {
		info := runGH(t, dir, "pr", "view", br, "--json", "state")
		if !strings.Contains(info, "OPEN") {
			t.Errorf("PR for %s should be OPEN after sync, got: %s", br, info)
		}
	}

	// Verify: the hotfix commit is now an ancestor of all stack branches
	t.Log("Verifying hotfix commit is reachable from all notification branches")
	runGit(t, dir, "fetch", "origin")
	for _, br := range []string{branchModel, branchService, branchUI} {
		// git merge-base --is-ancestor <main-tip> <branch>
		cmd := fmt.Sprintf("merge-base --is-ancestor %s origin/%s", newMainTip, br)
		args := strings.Fields(cmd)
		out, err := runGitMayFail(t, dir, args...)
		if err != nil {
			t.Errorf("hotfix commit %s is NOT an ancestor of %s (rebase didn't cascade): %s",
				newMainTip[:12], br, out)
		}
	}

	// Verify: stack state unchanged after sync (still 3 nodes, all open)
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/add-notification-model", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-notification-service", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-notification-ui", HasPR: true, Status: "open"},
	})

	t.Log("Sync verified — all notification branches rebased onto main with hotfix included")
}

// TestE2E_InsertBranchMidStack verifies that inserting a new branch in the
// middle of an existing stack correctly retargets PR bases and that a
// subsequent sync cascade-rebases downstream branches.
//
// Scenario: A developer builds a payment system in four stacked PRs:
//
//	main ← add-payment-model ← add-checkout-api ← add-checkout-ui ← add-payment-tests
//
// During review, they realize input validation is missing and insert a new
// branch between the model and the checkout API:
//
//	main ← add-payment-model ← add-payment-validation ← add-checkout-api ← add-checkout-ui ← add-payment-tests
//
// Verifications:
//   - add-payment-validation is inserted at the correct position
//   - add-checkout-api PR's base is retargeted to add-payment-validation
//   - After `sdf sync`, checkout-api/checkout-ui/payment-tests are rebased
//   - All PRs remain OPEN
func TestE2E_InsertBranchMidStack(t *testing.T) {
	dir := e2eRepo(t)
	setupRecording(t)
	prefix := testPrefix()

	t.Cleanup(func() {
		runGit(t, dir, "checkout", "main")
		cleanupPRs(t, dir, prefix)
		cleanupBranches(t, dir, prefix)
		os.RemoveAll(dir + "/.sdf")
	})

	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "pull", "origin", "main")

	stackName := prefix

	// Build a 4-branch payment system stack
	t.Log("Building payment stack: add-payment-model → add-checkout-api → add-checkout-ui → add-payment-tests")

	runSDF(t, dir, "init", "--base", "main", "--branch", "add-payment-model", stackName)
	writeCommit(t, dir, prefix+"-payment.go", "package model\n\ntype Payment struct {\n\tAmount   int\n\tCurrency string\n}\n", "feat: add payment data model")

	runSDF(t, dir, "branch", "add-checkout-api")
	writeCommit(t, dir, prefix+"-checkout.go", "package api\n\nfunc CheckoutHandler(w http.ResponseWriter, r *http.Request) {}\n", "feat: add checkout API endpoint")

	runSDF(t, dir, "branch", "add-checkout-ui")
	writeCommit(t, dir, prefix+"-checkout-ui.tsx", "export const CheckoutForm = () => <form><button>Pay Now</button></form>;\n", "feat: add checkout form component")

	runSDF(t, dir, "branch", "add-payment-tests")
	writeCommit(t, dir, prefix+"-payment_test.go", "package payment_test\n\nfunc TestCheckout(t *testing.T) {}\n", "test: add checkout integration tests")

	branchModel := stackName + "/add-payment-model"
	branchCheckoutAPI := stackName + "/add-checkout-api"
	branchCheckoutUI := stackName + "/add-checkout-ui"
	branchTests := stackName + "/add-payment-tests"
	branchValidation := stackName + "/add-payment-validation"

	// Create PRs for the initial 4 branches
	t.Log("Creating PRs for add-payment-model, add-checkout-api, add-checkout-ui, add-payment-tests")
	prNumbers := make(map[string]int)
	for _, br := range []string{branchModel, branchCheckoutAPI, branchCheckoutUI, branchTests} {
		runGit(t, dir, "checkout", br)
		out := runSDF(t, dir, "pr", "--json")

		var result struct {
			Number int `json:"number"`
		}
		json.Unmarshal([]byte(out), &result)
		prNumbers[br] = result.Number
		t.Logf("PR#%d created for %s", result.Number, br)
	}

	// Verify: 4 nodes with PRs
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/add-payment-model", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-checkout-api", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-checkout-ui", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-payment-tests", HasPR: true, Status: "open"},
	})

	// Verify checkout-api PR targets the payment-model branch
	checkoutAPIBase := runGH(t, dir, "pr", "view", fmt.Sprint(prNumbers[branchCheckoutAPI]), "--json", "baseRefName")
	if !strings.Contains(checkoutAPIBase, "add-payment-model") {
		t.Fatalf("before insert: PR for add-checkout-api should have base containing add-payment-model, got: %s", checkoutAPIBase)
	}

	// Insert add-payment-validation between model and checkout-api
	t.Log("Inserting add-payment-validation between add-payment-model and add-checkout-api")

	// Checkout the model branch so `sdf branch` inserts after it
	runGit(t, dir, "checkout", branchModel)
	runSDF(t, dir, "branch", "add-payment-validation")

	// Verify we're on the new branch
	current := runGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if current != branchValidation {
		t.Fatalf("expected to be on %s after insert, got %s", branchValidation, current)
	}

	// Add validation logic
	writeCommit(t, dir, prefix+"-validate.go", "package validation\n\nfunc ValidatePayment(amount int, currency string) error {\n\tif amount <= 0 { return fmt.Errorf(\"invalid amount\") }\n\treturn nil\n}\n", "feat: add payment amount and currency validation")

	// Create a PR for the new validation branch
	valOut := runSDF(t, dir, "pr", "--json")
	var valResult struct {
		Number int `json:"number"`
	}
	json.Unmarshal([]byte(valOut), &valResult)
	prNumbers[branchValidation] = valResult.Number
	t.Logf("PR#%d created for %s (inserted)", valResult.Number, branchValidation)

	// Verify: 5 nodes — validation inserted between model and checkout-api
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/add-payment-model", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-payment-validation", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-checkout-api", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-checkout-ui", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-payment-tests", HasPR: true, Status: "open"},
	})

	// Verify: checkout-api PR's base is now the validation branch
	t.Log("Verifying add-checkout-api PR was retargeted to add-payment-validation")
	checkoutAPIBaseAfter := runGH(t, dir, "pr", "view", fmt.Sprint(prNumbers[branchCheckoutAPI]), "--json", "baseRefName")
	if !strings.Contains(checkoutAPIBaseAfter, "add-payment-validation") {
		t.Errorf("after insert: PR for add-checkout-api should have base containing add-payment-validation, got: %s", checkoutAPIBaseAfter)
	}

	// Sync to cascade-rebase downstream branches onto the new validation branch
	t.Log("Running sdf sync to cascade-rebase downstream branches through the inserted validation layer")
	runGit(t, dir, "checkout", branchTests)
	syncOut := runSDF(t, dir, "sync", "-y")
	t.Log(syncOut)

	// Verify: all 5 PRs still open
	t.Log("Verifying all PRs are still open")
	for _, br := range []string{branchModel, branchValidation, branchCheckoutAPI, branchCheckoutUI, branchTests} {
		info := runGH(t, dir, "pr", "view", br, "--json", "state")
		if !strings.Contains(info, "OPEN") {
			t.Errorf("PR for %s should be OPEN after sync, got: %s", br, info)
		}
	}

	// Verify: correct base chain after insert + sync
	t.Log("Verifying final base chain: model → validation → checkout-api → checkout-ui → tests")

	expectedBases := map[string]string{
		branchModel:       "main",
		branchValidation:  "add-payment-model",
		branchCheckoutAPI: "add-payment-validation",
		branchCheckoutUI:  "add-checkout-api",
		branchTests:       "add-checkout-ui",
	}

	for br, expectedBase := range expectedBases {
		prNum := prNumbers[br]
		info := runGH(t, dir, "pr", "view", fmt.Sprint(prNum), "--json", "baseRefName")

		var result struct {
			BaseRefName string `json:"baseRefName"`
		}
		json.Unmarshal([]byte(info), &result)

		if !strings.Contains(result.BaseRefName, expectedBase) {
			t.Errorf("PR for %s: expected base containing %q, got %q", br, expectedBase, result.BaseRefName)
		}
	}

	// Verify: validation branch's commit is reachable from all downstream branches
	t.Log("Verifying validation code is reachable from all downstream branches")
	valTip := runGit(t, dir, "rev-parse", branchValidation)
	runGit(t, dir, "fetch", "origin")
	for _, br := range []string{branchCheckoutAPI, branchCheckoutUI, branchTests} {
		args := []string{"merge-base", "--is-ancestor", valTip, "origin/" + br}
		_, err := runGitMayFail(t, dir, args...)
		if err != nil {
			t.Errorf("validation tip is NOT an ancestor of %s — rebase didn't cascade through insert point", br)
		}
	}

	t.Log("Insert verified — model → validation → checkout-api → checkout-ui → tests with correct bases")
}

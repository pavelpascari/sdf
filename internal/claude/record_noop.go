//go:build !spyrecord

package claude

// recordRun is a no-op in production builds.
// With -tags spyrecord, the real implementation records invocations.
func recordRun(args []string, output string, exitCode int) {}

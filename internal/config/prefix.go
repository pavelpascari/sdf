package config

import "strings"

// ApplyPrefix prepends the configured prefix and separator to branchName.
//
// Returns branchName unchanged if:
//   - prefix enforcement is disabled
//   - branchName already starts with prefix+separator (no double-prefix)
func ApplyPrefix(cfg Config, stackID, branchName string) string {
	if !cfg.IsEnabled() {
		return branchName
	}

	prefix := cfg.EffectivePrefix(stackID)
	if prefix == "" {
		return branchName
	}

	sep := cfg.EffectiveSeparator()
	fullPrefix := prefix + sep

	// Guard against double-prefixing
	if strings.HasPrefix(branchName, fullPrefix) {
		return branchName
	}

	return fullPrefix + branchName
}

// HasPrefix returns true if the branch name already has the configured prefix.
func HasPrefix(cfg Config, stackID, branchName string) bool {
	if !cfg.IsEnabled() {
		return false
	}

	prefix := cfg.EffectivePrefix(stackID)
	if prefix == "" {
		return false
	}

	sep := cfg.EffectiveSeparator()
	return strings.HasPrefix(branchName, prefix+sep)
}

// StripPrefix removes the prefix+separator from a branch name, if present.
func StripPrefix(cfg Config, stackID, branchName string) string {
	if !cfg.IsEnabled() {
		return branchName
	}

	prefix := cfg.EffectivePrefix(stackID)
	if prefix == "" {
		return branchName
	}

	sep := cfg.EffectiveSeparator()
	fullPrefix := prefix + sep

	if strings.HasPrefix(branchName, fullPrefix) {
		return branchName[len(fullPrefix):]
	}

	return branchName
}

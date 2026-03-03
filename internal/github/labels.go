package github

import (
	"fmt"
	"strings"

	"github.com/google/go-github/v81/github"
)

// ParseTargetBranches extracts target branch names from PR labels that match the pattern.
// Example: label "pronto/release-1.0" with pattern "pronto/" returns "release-1.0"
func ParseTargetBranches(labels []*github.Label, pattern string) []string {
	var branches []string
	seen := make(map[string]bool)

	for _, label := range labels {
		if label.Name == nil {
			continue
		}

		labelName := *label.Name

		// Check if label matches the pattern
		if !strings.HasPrefix(labelName, pattern) {
			continue
		}

		// Extract branch name by removing the pattern prefix
		branchName := strings.TrimPrefix(labelName, pattern)

		// Skip empty branch names or invalid patterns
		if branchName == "" || branchName == labelName {
			continue
		}

		// Validate branch name format (basic validation)
		if !isValidBranchName(branchName) {
			continue
		}

		// Deduplicate branches
		if !seen[branchName] {
			branches = append(branches, branchName)
			seen[branchName] = true
		}
	}

	return branches
}

// isValidBranchName performs basic validation on branch names.
// Prevents obviously invalid branch names while being permissive.
func isValidBranchName(name string) bool {
	if name == "" {
		return false
	}

	// Branch name cannot start or end with a slash
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return false
	}

	// Branch name cannot contain certain invalid characters
	invalid := []string{"..", "~", "^", ":", "?", "*", "[", "\\", " "}
	for _, char := range invalid {
		if strings.Contains(name, char) {
			return false
		}
	}

	return true
}

// FormatProntoLabel creates a pronto label for a target branch.
func FormatProntoLabel(pattern, branchName string) string {
	return fmt.Sprintf("%s%s", pattern, branchName)
}

// IsProntoLabel checks if a label matches the pronto pattern.
func IsProntoLabel(labelName, pattern string) bool {
	return strings.HasPrefix(labelName, pattern)
}

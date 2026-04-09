package github

import (
	"fmt"
	"strings"

	"github.com/google/go-github/v81/github"
	"github.com/theakshaypant/pronto/pkg/models"
)

// ParseTargetBranches extracts target branch information from PR labels that match the pattern.
// Supports formats:
//   - "pronto/release-1.0" - cherry-pick to existing branch
//   - "pronto/release-1.0..main" - create release-1.0 from main, then cherry-pick
//   - "pronto/release-1.0?tag=v1.0.1" - cherry-pick and create tag v1.0.1
//   - "pronto/release-1.0..main?tag=v1.0.0" - create branch, cherry-pick, and create tag
func ParseTargetBranches(labels []*github.Label, pattern string) []*models.TargetBranch {
	var branches []*models.TargetBranch
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

		// Extract branch spec by removing the pattern prefix
		branchSpec := strings.TrimPrefix(labelName, pattern)

		// Skip empty branch names or invalid patterns
		if branchSpec == "" || branchSpec == labelName {
			continue
		}

		// Parse the branch spec for @ notation
		targetBranch := ParseBranchSpec(branchSpec)
		if targetBranch == nil {
			continue
		}

		// Deduplicate branches by target name
		if !seen[targetBranch.Name] {
			branches = append(branches, targetBranch)
			seen[targetBranch.Name] = true
		}
	}

	return branches
}

// ParseBranchSpec parses a branch specification string into a TargetBranch.
// Formats:
//   - "release-1.0" -> TargetBranch{Name: "release-1.0"}
//   - "release-1.0..main" -> TargetBranch{Name: "release-1.0", BaseBranch: "main", ShouldCreate: true}
//   - "release-1.0?tag=v1.0.1" -> TargetBranch{Name: "release-1.0", TagName: "v1.0.1"}
//   - "release-1.0..main?tag=v1.0.0" -> TargetBranch{Name: "release-1.0", BaseBranch: "main", ShouldCreate: true, TagName: "v1.0.0"}
func ParseBranchSpec(spec string) *models.TargetBranch {
	var targetName, baseBranch, tagName string
	shouldCreate := false

	// First, check for ?tag= notation (Git doesn't allow ? in branch names)
	branchPart := spec
	hasTagNotation := strings.Contains(spec, "?tag=")
	if hasTagNotation {
		parts := strings.SplitN(spec, "?tag=", 2)
		if len(parts) == 2 {
			branchPart = parts[0]
			tagName = parts[1]
		}
	}

	// Check for .. notation in the branch part (Git doesn't allow .. in branch names)
	if strings.Contains(branchPart, "..") {
		parts := strings.SplitN(branchPart, "..", 2)
		if len(parts) != 2 {
			return nil
		}
		targetName = parts[0]
		baseBranch = parts[1]
		shouldCreate = true
	} else {
		targetName = branchPart
	}

	// Validate target branch name
	if !isValidBranchName(targetName) {
		return nil
	}

	// Validate base branch name if specified
	if shouldCreate && !isValidBranchName(baseBranch) {
		return nil
	}

	// Validate tag name if ?tag= notation was used
	if hasTagNotation {
		// If ?tag= is present, tag name must be non-empty and valid
		if tagName == "" || !isValidTagName(tagName) {
			return nil
		}
	}

	return &models.TargetBranch{
		Name:         targetName,
		BaseBranch:   baseBranch,
		ShouldCreate: shouldCreate,
		TagName:      tagName,
	}
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

// isValidTagName performs basic validation on tag names.
// Tag names follow similar rules to branch names but are slightly more permissive.
func isValidTagName(name string) bool {
	if name == "" {
		return false
	}

	// Tag name cannot start with a period or hyphen
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "-") {
		return false
	}

	// Tag name cannot contain certain invalid characters
	invalid := []string{"..", "~", "^", ":", "?", "*", "[", "\\", " ", "@{"}
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

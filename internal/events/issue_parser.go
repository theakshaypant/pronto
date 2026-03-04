package events

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	ghclient "github.com/theakshaypant/pronto/internal/github"
)

// prNumberRegex matches PR references in markdown (e.g., #123, #456)
var prNumberRegex = regexp.MustCompile(`#(\d+)`)

// ParsePRNumbers extracts PR numbers from markdown text.
// It handles various formats: #123, #123, #456, #123 and #456, etc.
// Returns deduplicated PR numbers in the order they first appear.
func ParsePRNumbers(text string) []int {
	matches := prNumberRegex.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	// Use a map to deduplicate while preserving order
	seen := make(map[int]bool)
	var prNumbers []int

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		// Convert string to int
		prNum, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}

		// Skip if already seen
		if seen[prNum] {
			continue
		}

		seen[prNum] = true
		prNumbers = append(prNumbers, prNum)
	}

	return prNumbers
}

// PRValidationResult contains the result of validating a single PR.
type PRValidationResult struct {
	Number int
	Exists bool
	Merged bool
	Error  error
}

// ValidatePRs checks if PRs exist and are merged using the GitHub API.
// Returns a slice of validation results, one for each PR number.
func ValidatePRs(ctx context.Context, client *ghclient.Client, prNumbers []int) []PRValidationResult {
	results := make([]PRValidationResult, len(prNumbers))

	for i, prNum := range prNumbers {
		result := PRValidationResult{
			Number: prNum,
		}

		// Get the PR from GitHub API
		pr, err := client.GetPullRequest(ctx, prNum)
		if err != nil {
			result.Error = fmt.Errorf("failed to get PR #%d: %w", prNum, err)
			results[i] = result
			continue
		}

		result.Exists = true
		result.Merged = pr.GetMerged()
		results[i] = result
	}

	return results
}

// FilterMergedPRs returns only the PR numbers that are merged.
// Non-existent or unmerged PRs are filtered out.
func FilterMergedPRs(validationResults []PRValidationResult) []int {
	var mergedPRs []int

	for _, result := range validationResults {
		if result.Exists && result.Merged && result.Error == nil {
			mergedPRs = append(mergedPRs, result.Number)
		}
	}

	return mergedPRs
}

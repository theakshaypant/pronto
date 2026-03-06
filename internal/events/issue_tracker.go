package events

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/google/go-github/v81/github"
	ghclient "github.com/theakshaypant/pronto/internal/github"
)

const (
	// StatusTableMarker is used to identify PROnto status table comments
	StatusTableMarker = "## 🤖 PROnto Batch Summary"
)

// TrackingIssue represents an issue that tracks PR cherry-picking operations.
type TrackingIssue struct {
	Number  int
	Issue   *github.Issue
	PRNums  []int // PR numbers mentioned in the issue body
}

// FindTrackingIssues searches for open issues with pronto/* labels that reference a specific PR.
func FindTrackingIssues(ctx context.Context, client *ghclient.Client, prNumber int, labelPattern string) ([]*TrackingIssue, error) {
	// Search for open issues with pronto/* labels
	// We can't directly search for PR numbers in issue bodies via API,
	// so we search for all open issues and filter client-side
	query := fmt.Sprintf("is:open is:issue label:%s*", labelPattern)

	log.Printf("Searching for tracking issues with query: %s", query)

	issues, err := client.SearchIssues(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to search issues: %w", err)
	}

	log.Printf("Found %d open issues with pronto labels", len(issues))

	var trackingIssues []*TrackingIssue

	for _, issue := range issues {
		// Parse PR numbers from issue body
		prNums := ParsePRNumbers(issue.GetBody())

		// Check if this issue references the target PR
		if contains(prNums, prNumber) {
			trackingIssues = append(trackingIssues, &TrackingIssue{
				Number: issue.GetNumber(),
				Issue:  issue,
				PRNums: prNums,
			})
			log.Printf("Issue #%d references PR #%d", issue.GetNumber(), prNumber)
		}
	}

	log.Printf("Found %d tracking issues for PR #%d", len(trackingIssues), prNumber)
	return trackingIssues, nil
}

// UpdateTrackingIssue updates the status table in a tracking issue for a specific PR+branch combination.
func UpdateTrackingIssue(ctx context.Context, client *ghclient.Client, issueNumber int, prNumber int, targetBranch string, newStatus, newMessage string) error {
	log.Printf("Updating tracking issue #%d for PR #%d on branch %s", issueNumber, prNumber, targetBranch)

	// Find the existing status table comment
	existingComment, err := client.FindProntoComment(ctx, issueNumber, StatusTableMarker)
	if err != nil {
		return fmt.Errorf("failed to find status table comment: %w", err)
	}

	if existingComment == nil {
		// No existing comment - this shouldn't happen in normal flow, but we can create one
		log.Printf("No existing status table found for issue #%d, skipping update", issueNumber)
		return nil
	}

	// Parse the existing table
	currentBody := existingComment.GetBody()
	updatedBody, err := updateStatusTableRow(currentBody, prNumber, targetBranch, newStatus, newMessage)
	if err != nil {
		return fmt.Errorf("failed to update status table: %w", err)
	}

	// Update the comment
	if err := client.UpdateComment(ctx, existingComment.GetID(), updatedBody); err != nil {
		return fmt.Errorf("failed to update comment: %w", err)
	}

	log.Printf("Successfully updated status table in issue #%d", issueNumber)
	return nil
}

// updateStatusTableRow updates a specific row in the status table markdown.
func updateStatusTableRow(tableBody string, prNumber int, targetBranch string, newStatus, newMessage string) (string, error) {
	lines := strings.Split(tableBody, "\n")

	// Find the table section
	tableStart := -1
	tableEnd := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Find table header
		if strings.HasPrefix(trimmed, "| PR |") || strings.HasPrefix(trimmed, "| PR|") {
			tableStart = i
			continue
		}

		// Find summary section (marks end of table)
		if tableStart != -1 && strings.HasPrefix(trimmed, "**Summary:**") {
			tableEnd = i
			break
		}
	}

	if tableStart == -1 {
		return "", fmt.Errorf("status table not found in comment")
	}

	if tableEnd == -1 {
		tableEnd = len(lines)
	}

	// Find and update the matching row
	rowUpdated := false
	for i := tableStart; i < tableEnd; i++ {
		line := lines[i]

		// Check if this row matches our PR + branch
		if matchesTableRow(line, prNumber, targetBranch) {
			// Update the row with new status and message
			lines[i] = buildTableRow(prNumber, targetBranch, newStatus, newMessage)
			rowUpdated = true
			log.Printf("Updated row for PR #%d, branch %s", prNumber, targetBranch)
			break
		}
	}

	if !rowUpdated {
		log.Printf("Warning: No matching row found for PR #%d, branch %s", prNumber, targetBranch)
		// Row not found - this is OK, might not be in this issue
		return tableBody, nil
	}

	// Recalculate summary counts
	updatedBody := strings.Join(lines, "\n")
	updatedBody = recalculateSummary(updatedBody)

	return updatedBody, nil
}

// matchesTableRow checks if a markdown table row matches the given PR and branch.
func matchesTableRow(line string, prNumber int, targetBranch string) bool {
	// Table row format: | [#123](#123) | `release-1.0` | ... |
	// We need to match both PR number and branch

	// Extract PR number from row
	prPattern := regexp.MustCompile(`\|\s*\[#(\d+)\]`)
	prMatches := prPattern.FindStringSubmatch(line)
	if len(prMatches) < 2 {
		return false
	}

	rowPR := prMatches[1]
	if rowPR != fmt.Sprintf("%d", prNumber) {
		return false
	}

	// Extract branch from row
	// Table row format: | `branch-name` |
	backtick := "`"
	branchPatternStr := `\|\s*` + backtick + `([^` + backtick + `]+)` + backtick + `\s*\|`
	branchPattern := regexp.MustCompile(branchPatternStr)
	branchMatches := branchPattern.FindStringSubmatch(line)
	if len(branchMatches) < 2 {
		return false
	}

	rowBranch := branchMatches[1]
	return rowBranch == targetBranch
}

// buildTableRow constructs a markdown table row with the given values.
func buildTableRow(prNumber int, targetBranch string, status string, message string) string {
	// Map status to emoji
	statusEmoji := ""
	switch status {
	case "success":
		statusEmoji = "✅"
	case "failed":
		statusEmoji = "❌"
	case "skipped":
		statusEmoji = "⏭️"
	case "pending_pr":
		statusEmoji = "🔄"
	case "conflict":
		statusEmoji = "⚠️"
	default:
		statusEmoji = "❓"
	}

	return fmt.Sprintf("| [#%d](#%d) | `%s` | %s | %s |",
		prNumber, prNumber, targetBranch, statusEmoji, message)
}

// recalculateSummary updates the summary section counts based on current table state.
func recalculateSummary(body string) string {
	lines := strings.Split(body, "\n")

	// Count statuses from table rows
	successCount := 0
	failedCount := 0
	skippedCount := 0
	pendingCount := 0
	conflictCount := 0

	for _, line := range lines {
		if strings.Contains(line, "| ✅ |") {
			successCount++
		} else if strings.Contains(line, "| ❌ |") {
			failedCount++
		} else if strings.Contains(line, "| ⏭️ |") {
			skippedCount++
		} else if strings.Contains(line, "| 🔄 |") {
			pendingCount++
		} else if strings.Contains(line, "| ⚠️ |") {
			conflictCount++
		}
	}

	// Find and replace summary section
	summaryStart := -1
	summaryEnd := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "**Summary:**") {
			summaryStart = i
		}
		if summaryStart != -1 && strings.HasPrefix(trimmed, "---") {
			summaryEnd = i
			break
		}
	}

	if summaryStart == -1 {
		return body // No summary section found
	}

	// Build new summary
	var newSummary []string
	newSummary = append(newSummary, "**Summary:**")

	if successCount > 0 {
		newSummary = append(newSummary, fmt.Sprintf("- ✅ Success: %d", successCount))
	}
	if pendingCount > 0 {
		newSummary = append(newSummary, fmt.Sprintf("- 🔄 Pending PR merge: %d", pendingCount))
	}
	if conflictCount > 0 {
		newSummary = append(newSummary, fmt.Sprintf("- ⚠️ Conflicts (manual resolution needed): %d", conflictCount))
	}
	if failedCount > 0 {
		newSummary = append(newSummary, fmt.Sprintf("- ❌ Failed: %d", failedCount))
	}
	if skippedCount > 0 {
		newSummary = append(newSummary, fmt.Sprintf("- ⏭️ Skipped: %d", skippedCount))
	}

	// Replace summary section
	result := append(lines[:summaryStart], newSummary...)
	if summaryEnd != -1 {
		result = append(result, "")
		result = append(result, lines[summaryEnd:]...)
	}

	return strings.Join(result, "\n")
}

// Helper function to check if a slice contains an integer
func contains(slice []int, item int) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

package events

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/google/go-github/v81/github"
	"github.com/theakshaypant/pronto/internal/action"
	"github.com/theakshaypant/pronto/internal/cherrypick"
	"github.com/theakshaypant/pronto/internal/deduplication"
	ghclient "github.com/theakshaypant/pronto/internal/github"
	"github.com/theakshaypant/pronto/internal/permissions"
	"github.com/theakshaypant/pronto/pkg/models"
)

// PRProcessor handles pull request events for cherry-picking.
type PRProcessor struct {
	ctx         context.Context
	config      *action.Config
	ghClient    *ghclient.Client
	permChecker *permissions.Checker
	tracker     *deduplication.Tracker
	event       *github.PullRequestEvent
}

// ProcessResult tracks the result of processing a single target branch.
type ProcessResult struct {
	TargetBranch string
	Success      bool
	Message      string
	CreatedPR    int // PR number if a new PR was created
}

// NewPRProcessor creates a new pull request processor.
func NewPRProcessor(ctx context.Context, cfg *action.Config, event *github.PullRequestEvent) (*PRProcessor, error) {
	// Perform comprehensive validation
	if err := validatePREvent(event); err != nil {
		return nil, fmt.Errorf("invalid pull request event: %w", err)
	}

	owner := event.Repo.GetOwner().GetLogin()
	repo := event.Repo.GetName()

	// Create GitHub client
	client, err := ghclient.NewClient(ctx, cfg.GitHubToken, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Create permission checker
	permChecker := permissions.NewChecker(client.GetClient(), owner, repo)

	// Create deduplication tracker
	tracker := deduplication.NewTracker()

	return &PRProcessor{
		ctx:         ctx,
		config:      cfg,
		ghClient:    client,
		permChecker: permChecker,
		tracker:     tracker,
		event:       event,
	}, nil
}

// Process handles the pull request event.
func (p *PRProcessor) Process(action EventAction) error {
	pr := p.event.PullRequest

	// Only process merged PRs
	if !pr.GetMerged() {
		log.Printf("PR #%d is not merged, skipping", pr.GetNumber())
		return nil
	}

	log.Printf("Processing merged PR #%d", pr.GetNumber())

	// Parse pronto/* labels to get target branches
	targetBranches := ghclient.ParseTargetBranches(pr.Labels, p.config.LabelPattern)

	// If no labels on PR, check for tracking issues that reference this PR
	if len(targetBranches) == 0 {
		log.Printf("No pronto labels found on PR #%d, checking for tracking issues", pr.GetNumber())

		trackingIssues, err := FindTrackingIssues(p.ctx, p.ghClient, pr.GetNumber(), p.config.LabelPattern)
		if err != nil {
			log.Printf("Error finding tracking issues: %v", err)
		} else if len(trackingIssues) > 0 {
			log.Printf("Found %d tracking issue(s) that reference this PR", len(trackingIssues))

			// Process cherry-picks for each tracking issue
			for _, issue := range trackingIssues {
				if err := p.processTrackingIssue(issue, pr.GetNumber()); err != nil {
					log.Printf("Error processing tracking issue #%d: %v", issue.Number, err)
				}
			}

			return nil
		}

		log.Printf("No tracking issues found for PR #%d, skipping", pr.GetNumber())
		return nil
	}

	log.Printf("Found %d target branch(es) from PR labels: %v", len(targetBranches), targetBranches)

	// Get the user who triggered the action
	username := p.getUserForPermissionCheck(action)
	log.Printf("Checking permissions for user: %s", username)

	// Check if user has write permissions
	hasWriteAccess, err := p.permChecker.HasWriteAccess(p.ctx, username)
	if err != nil {
		return fmt.Errorf("failed to check permissions for user %s: %w", username, err)
	}

	log.Printf("User %s has write access: %t", username, hasWriteAccess)

	// Get commits from the PR
	commits, err := p.ghClient.GetPullRequestCommits(p.ctx, pr.GetNumber())
	if err != nil {
		return fmt.Errorf("failed to get PR commits: %w", err)
	}

	// Validate commits
	if err := validateCommits(commits); err != nil {
		return fmt.Errorf("invalid commits in PR: %w", err)
	}

	commitSHAs := make([]string, len(commits))
	commitMessages := make([]string, len(commits))
	for i, commit := range commits {
		commitSHAs[i] = commit.GetSHA()
		commitMessages[i] = fmt.Sprintf("%s (%s)", commit.Commit.GetMessage(), commit.GetSHA()[:7])
	}

	log.Printf("PR has %d commit(s)", len(commitSHAs))

	// Process each target branch and collect results
	var results []ProcessResult
	for _, targetBranch := range targetBranches {
		log.Printf("Processing target branch: %s", targetBranch.Name)

		result := p.processTargetBranch(targetBranch, commitSHAs, commitMessages, hasWriteAccess)
		results = append(results, result)

		if !result.Success {
			log.Printf("Error processing branch %s: %s", targetBranch.Name, result.Message)
		}
	}

	// Create a single summary comment with all results
	if len(results) > 0 {
		if err := p.createSummaryComment(results); err != nil {
			log.Printf("Failed to create summary comment: %v", err)
		}
	}

	// Update tracking issues if this is a cherry-pick PR merge
	if err := p.updateTrackingIssues(); err != nil {
		log.Printf("Failed to update tracking issues: %v", err)
	}

	return nil
}

// processTargetBranch handles cherry-picking to a single target branch.
// It delegates the core logic to the cherrypick.Service.
func (p *PRProcessor) processTargetBranch(target *models.TargetBranch, commitSHAs, commitMessages []string, hasWriteAccess bool) ProcessResult {
	pr := p.event.PullRequest
	targetBranch := target.Name

	// Check deduplication - prevent processing the same PR/branch/SHA combination
	headSHA := pr.GetHead().GetSHA()
	if !p.tracker.ShouldProcess(pr.GetNumber(), targetBranch, headSHA) {
		log.Printf("Already processed PR #%d to branch %s at SHA %s, skipping", pr.GetNumber(), targetBranch, headSHA[:7])
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      true,
			Message:      "⏭️ Already processed (skipped duplicate)",
		}
	}

	// Mark as processed to prevent duplicate processing from webhook retries
	defer p.tracker.MarkProcessed(pr.GetNumber(), targetBranch, headSHA)

	// Build options for the cherry-pick service
	opts := cherrypick.CherryPickOptions{
		Owner:          p.event.Repo.GetOwner().GetLogin(),
		Repo:           p.event.Repo.GetName(),
		CloneURL:       p.event.Repo.GetCloneURL(),
		Token:          p.config.GitHubToken,
		PRNumber:       pr.GetNumber(),
		TargetBranch:   target,
		CommitSHAs:     commitSHAs,
		CommitMessages: commitMessages,
		HasWriteAccess: hasWriteAccess,
		AlwaysCreatePR: p.config.AlwaysCreatePR,
		BotName:        p.config.BotName,
		BotEmail:       p.config.BotEmail,
		ConflictLabel:  p.config.ConflictLabel,
		LabelPattern:   p.config.LabelPattern,
	}

	svc := cherrypick.NewService(p.ctx, p.ghClient)
	result := svc.CherryPickToTarget(opts)

	return ProcessResult{
		TargetBranch: result.TargetBranch,
		Success:      result.Success,
		Message:      result.Message,
		CreatedPR:    result.CreatedPR,
	}
}

// createSummaryComment creates a single summary comment with all processing results.
func (p *PRProcessor) createSummaryComment(results []ProcessResult) error {
	pr := p.event.PullRequest

	var sb strings.Builder
	sb.WriteString("## 🤖 PROnto Summary\n\n")

	// Group results by success/failure
	var successes, failures []ProcessResult
	for _, r := range results {
		if r.Success {
			successes = append(successes, r)
		} else {
			failures = append(failures, r)
		}
	}

	// Show successes first
	if len(successes) > 0 {
		for _, r := range successes {
			fmt.Fprintf(&sb, "**`%s`**: %s\n\n", r.TargetBranch, r.Message)
		}
	}

	// Then show failures
	if len(failures) > 0 {
		for _, r := range failures {
			fmt.Fprintf(&sb, "**`%s`**: %s\n\n", r.TargetBranch, r.Message)
		}
	}

	sb.WriteString("---\n")
	sb.WriteString("🤖 Automated by [PROnto](https://github.com/theakshaypant/pronto)")

	comment := sb.String()

	if err := p.ghClient.AddComment(p.ctx, pr.GetNumber(), comment); err != nil {
		return fmt.Errorf("failed to add summary comment: %w", err)
	}

	log.Printf("Added summary comment to PR #%d with %d results", pr.GetNumber(), len(results))
	return nil
}

// updateTrackingIssues updates tracking issues when a cherry-pick PR merges.
func (p *PRProcessor) updateTrackingIssues() error {
	pr := p.event.PullRequest

	// Check if this is a PROnto-created cherry-pick PR
	// These PRs have the "pronto" label and follow the branch naming pattern: pronto/{branch}/pr-{num}
	if !p.isProntoCherryPickPR() {
		return nil // Not a cherry-pick PR, nothing to do
	}

	// Extract original PR number and target branch from branch name
	branchName := pr.GetHead().GetRef()
	originalPR, targetBranch, err := parseCherryPickBranchName(branchName)
	if err != nil {
		log.Printf("Failed to parse cherry-pick branch name %s: %v", branchName, err)
		return nil // Invalid branch name format, skip
	}

	log.Printf("Cherry-pick PR #%d merged: original PR #%d, target branch %s",
		pr.GetNumber(), originalPR, targetBranch)

	// Find tracking issues that reference the original PR
	trackingIssues, err := FindTrackingIssues(p.ctx, p.ghClient, originalPR, p.config.LabelPattern)
	if err != nil {
		return fmt.Errorf("failed to find tracking issues: %w", err)
	}

	if len(trackingIssues) == 0 {
		log.Printf("No tracking issues found for PR #%d", originalPR)
		return nil
	}

	log.Printf("Found %d tracking issue(s) for PR #%d", len(trackingIssues), originalPR)

	// Update each tracking issue
	for _, issue := range trackingIssues {
		newMessage := fmt.Sprintf("Cherry-picked via [PR #%d](#%d)", pr.GetNumber(), pr.GetNumber())

		if err := UpdateTrackingIssue(p.ctx, p.ghClient, issue.Number, originalPR, targetBranch, "success", newMessage); err != nil {
			log.Printf("Failed to update tracking issue #%d: %v", issue.Number, err)
			continue
		}

		log.Printf("Successfully updated tracking issue #%d", issue.Number)
	}

	return nil
}

// isProntoCherryPickPR checks if the current PR is a PROnto-created cherry-pick PR.
func (p *PRProcessor) isProntoCherryPickPR() bool {
	pr := p.event.PullRequest

	// Check for "pronto" label
	for _, label := range pr.Labels {
		if label.GetName() == "pronto" {
			// Check branch name pattern
			branchName := pr.GetHead().GetRef()
			if strings.HasPrefix(branchName, "pronto/") && strings.Contains(branchName, "/pr-") {
				return true
			}
		}
	}

	return false
}

// parseCherryPickBranchName extracts the original PR number and target branch from a cherry-pick branch name.
// Format: pronto/{target-branch}/pr-{original-pr-number}
// Example: pronto/release-1.0/pr-123 -> (123, "release-1.0", nil)
func parseCherryPickBranchName(branchName string) (prNumber int, targetBranch string, err error) {
	// Pattern: pronto/{branch}/pr-{num}
	parts := strings.Split(branchName, "/")
	if len(parts) < 3 {
		return 0, "", fmt.Errorf("invalid branch name format: expected pronto/{branch}/pr-{num}")
	}

	if parts[0] != "pronto" {
		return 0, "", fmt.Errorf("branch does not start with pronto/")
	}

	// Extract PR number from last part (pr-{num})
	prPart := parts[len(parts)-1]
	if !strings.HasPrefix(prPart, "pr-") {
		return 0, "", fmt.Errorf("invalid PR part: expected pr-{num}")
	}

	prNumStr := strings.TrimPrefix(prPart, "pr-")
	prNum, err := fmt.Sscanf(prNumStr, "%d", &prNumber)
	if err != nil || prNum != 1 {
		return 0, "", fmt.Errorf("invalid PR number: %s", prNumStr)
	}

	// Extract target branch (everything between pronto/ and /pr-{num})
	targetBranch = strings.Join(parts[1:len(parts)-1], "/")

	return prNumber, targetBranch, nil
}

// processTrackingIssue processes cherry-picks for a PR when it's referenced in a tracking issue.
func (p *PRProcessor) processTrackingIssue(trackingIssue *TrackingIssue, prNumber int) error {
	log.Printf("Processing tracking issue #%d for PR #%d", trackingIssue.Number, prNumber)

	// Get target branches from issue labels
	targetBranches := ghclient.ParseTargetBranches(trackingIssue.Issue.Labels, p.config.LabelPattern)
	if len(targetBranches) == 0 {
		log.Printf("No pronto labels found on tracking issue #%d", trackingIssue.Number)
		return nil
	}

	log.Printf("Found %d target branch(es) from tracking issue: %v", len(targetBranches), targetBranches)

	// Get the PR
	pr, err := p.ghClient.GetPullRequest(p.ctx, prNumber)
	if err != nil {
		return fmt.Errorf("failed to get PR #%d: %w", prNumber, err)
	}

	// Get commits from the PR
	commits, err := p.ghClient.GetPullRequestCommits(p.ctx, prNumber)
	if err != nil {
		return fmt.Errorf("failed to get PR commits: %w", err)
	}

	// Validate commits
	if err := validateCommits(commits); err != nil {
		return fmt.Errorf("invalid commits in PR: %w", err)
	}

	commitSHAs := make([]string, len(commits))
	commitMessages := make([]string, len(commits))
	for i, commit := range commits {
		commitSHAs[i] = commit.GetSHA()
		commitMessages[i] = fmt.Sprintf("%s (%s)", commit.Commit.GetMessage(), commit.GetSHA()[:7])
	}

	log.Printf("PR #%d has %d commit(s)", prNumber, len(commitSHAs))

	// Check permissions
	username := p.getUserForPermissionCheck(EventActionClosed)
	hasWriteAccess, err := p.permChecker.HasWriteAccess(p.ctx, username)
	if err != nil {
		log.Printf("Failed to check permissions for user %s: %v", username, err)
		hasWriteAccess = false
	}

	// Process each target branch using the cherry-pick service
	svc := cherrypick.NewService(p.ctx, p.ghClient)

	var results []BatchResult
	for _, targetBranch := range targetBranches {
		log.Printf("Processing target branch: %s for PR #%d", targetBranch.Name, prNumber)

		// Check deduplication
		if p.tracker.IsProcessed(prNumber, targetBranch.Name, pr.GetHead().GetSHA()) {
			log.Printf("Skipping duplicate processing for PR #%d to %s", prNumber, targetBranch.Name)
			results = append(results, BatchResult{
				PRNumber:     prNumber,
				TargetBranch: targetBranch.Name,
				Success:      false,
				Status:       "skipped",
				Message:      "Already processed (duplicate)",
			})
			continue
		}

		// Mark as processed
		p.tracker.MarkProcessed(prNumber, targetBranch.Name, pr.GetHead().GetSHA())

		opts := cherrypick.CherryPickOptions{
			Owner:          p.event.Repo.GetOwner().GetLogin(),
			Repo:           p.event.Repo.GetName(),
			CloneURL:       p.event.Repo.GetCloneURL(),
			Token:          p.config.GitHubToken,
			PRNumber:       prNumber,
			TargetBranch:   targetBranch,
			CommitSHAs:     commitSHAs,
			CommitMessages: commitMessages,
			HasWriteAccess: hasWriteAccess,
			AlwaysCreatePR: p.config.AlwaysCreatePR,
			BotName:        p.config.BotName,
			BotEmail:       p.config.BotEmail,
			ConflictLabel:  p.config.ConflictLabel,
			LabelPattern:   p.config.LabelPattern,
		}

		cpResult := svc.CherryPickToTarget(opts)
		results = append(results, BatchResult{
			PRNumber:     cpResult.PRNumber,
			TargetBranch: cpResult.TargetBranch,
			Success:      cpResult.Success,
			Status:       cpResult.Status,
			Message:      cpResult.Message,
			CreatedPR:    cpResult.CreatedPR,
		})
	}

	// Update the tracking issue status table
	if len(results) > 0 {
		if err := p.updateTrackingIssueTable(trackingIssue.Number, results); err != nil {
			log.Printf("Failed to update tracking issue #%d: %v", trackingIssue.Number, err)
		}
	}

	return nil
}

// updateTrackingIssueTable updates the status table in a tracking issue with new results.
func (p *PRProcessor) updateTrackingIssueTable(issueNumber int, results []BatchResult) error {
	// Check if status table exists
	existingComment, err := p.ghClient.FindProntoComment(p.ctx, issueNumber, StatusTableMarker)
	if err != nil {
		log.Printf("Error finding existing status table: %v", err)
		return err
	}

	if existingComment == nil {
		// No status table exists - create one
		log.Printf("No existing status table found for issue #%d, creating initial table", issueNumber)
		return p.createInitialStatusTable(issueNumber, results)
	}

	// Status table exists - update individual rows
	log.Printf("Updating existing status table in issue #%d", issueNumber)
	for _, result := range results {
		status := result.Status
		message := result.Message

		if result.CreatedPR > 0 {
			switch status {
			case "pending_pr":
				message = fmt.Sprintf("Pending merge of [PR #%d](#%d)", result.CreatedPR, result.CreatedPR)
			case "conflict":
				message = fmt.Sprintf("Conflicts - see [PR #%d](#%d)", result.CreatedPR, result.CreatedPR)
			}
		}

		if err := UpdateTrackingIssue(p.ctx, p.ghClient, issueNumber, result.PRNumber, result.TargetBranch, status, message); err != nil {
			log.Printf("Failed to update row in tracking issue: %v", err)
		}
	}

	return nil
}

// createInitialStatusTable creates the initial status table when processing from PR merge.
func (p *PRProcessor) createInitialStatusTable(issueNumber int, results []BatchResult) error {
	var sb strings.Builder
	sb.WriteString("## 🤖 PROnto Batch Summary\n\n")

	// Count unique PRs and branches
	uniquePRs := make(map[int]bool)
	uniqueBranches := make(map[string]bool)
	for _, r := range results {
		uniquePRs[r.PRNumber] = true
		uniqueBranches[r.TargetBranch] = true
	}

	sb.WriteString(fmt.Sprintf("Processed %d PR(s) to %d unique branch(es):\n\n",
		len(uniquePRs), len(uniqueBranches)))

	// Create table
	sb.WriteString("| PR | Branch | Status | Details |\n")
	sb.WriteString("|----|--------|--------|---------|")

	var successCount, failedCount, skippedCount, pendingCount, conflictCount int

	for _, r := range results {
		sb.WriteString("\n")

		// Status emoji
		statusEmoji := ""
		switch r.Status {
		case "success":
			statusEmoji = "✅"
			successCount++
		case "failed":
			statusEmoji = "❌"
			failedCount++
		case "skipped":
			statusEmoji = "⏭️"
			skippedCount++
		case "pending_pr":
			statusEmoji = "🔄"
			pendingCount++
		case "conflict":
			statusEmoji = "⚠️"
			conflictCount++
		}

		// Format message
		msg := r.Message
		if r.CreatedPR > 0 {
			switch r.Status {
			case "conflict":
				msg = fmt.Sprintf("Conflicts - see [PR #%d](#%d)", r.CreatedPR, r.CreatedPR)
			case "pending_pr":
				msg = fmt.Sprintf("Pending merge of [PR #%d](#%d)", r.CreatedPR, r.CreatedPR)
			}
		}

		// Sanitize message for markdown table
		msg = sanitizeTableCell(msg)

		fmt.Fprintf(&sb, "| [#%d](#%d) | `%s` | %s | %s |",
			r.PRNumber, r.PRNumber, r.TargetBranch, statusEmoji, msg)
	}

	// Add summary
	sb.WriteString("\n\n**Summary:**\n")
	if successCount > 0 {
		fmt.Fprintf(&sb, "- ✅ Success: %d\n", successCount)
	}
	if pendingCount > 0 {
		fmt.Fprintf(&sb, "- 🔄 Pending PR merge: %d\n", pendingCount)
	}
	if conflictCount > 0 {
		fmt.Fprintf(&sb, "- ⚠️ Conflicts (manual resolution needed): %d\n", conflictCount)
	}
	if failedCount > 0 {
		fmt.Fprintf(&sb, "- ❌ Failed: %d\n", failedCount)
	}
	if skippedCount > 0 {
		fmt.Fprintf(&sb, "- ⏭️ Skipped: %d\n", skippedCount)
	}

	sb.WriteString("\n---\n")
	sb.WriteString("🤖 Automated by [PROnto](https://github.com/theakshaypant/pronto)")

	// Create the comment
	if err := p.ghClient.AddComment(p.ctx, issueNumber, sb.String()); err != nil {
		return fmt.Errorf("failed to create status table comment: %w", err)
	}

	log.Printf("✅ Created initial status table on issue #%d with %d results", issueNumber, len(results))
	return nil
}

// getUserForPermissionCheck determines which user to check permissions for.
func (p *PRProcessor) getUserForPermissionCheck(action EventAction) string {
	switch action {
	case EventActionLabeled:
		// For labeled events, check the user who added the label
		if p.event.Sender != nil && p.event.Sender.Login != nil {
			return *p.event.Sender.Login
		}
	case EventActionClosed:
		// For closed events, check the PR author
		if p.event.PullRequest != nil && p.event.PullRequest.User != nil && p.event.PullRequest.User.Login != nil {
			return *p.event.PullRequest.User.Login
		}
	}

	// Fallback to PR author
	if p.event.PullRequest != nil && p.event.PullRequest.User != nil && p.event.PullRequest.User.Login != nil {
		return *p.event.PullRequest.User.Login
	}

	return ""
}

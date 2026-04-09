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

// IssueProcessor handles issue events for batch cherry-picking.
type IssueProcessor struct {
	ctx         context.Context
	config      *action.Config
	ghClient    *ghclient.Client
	permChecker *permissions.Checker
	tracker     *deduplication.Tracker
	event       *github.IssuesEvent
}

// BatchResult tracks the result of processing a single PR+branch combination.
type BatchResult struct {
	PRNumber     int
	TargetBranch string
	Success      bool
	Status       string // "success", "failed", "skipped", "not_merged", "pending_pr"
	Message      string
	CreatedPR    int // PR number if cherry-pick PR created
}

// NewIssueProcessor creates a new issue processor.
func NewIssueProcessor(ctx context.Context, cfg *action.Config, event *github.IssuesEvent) (*IssueProcessor, error) {
	// Validate event
	if err := validateIssueEvent(event); err != nil {
		return nil, fmt.Errorf("invalid issue event: %w", err)
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

	return &IssueProcessor{
		ctx:         ctx,
		config:      cfg,
		ghClient:    client,
		permChecker: permChecker,
		tracker:     tracker,
		event:       event,
	}, nil
}

// Process handles the issue event.
func (p *IssueProcessor) Process(action EventAction) error {
	issue := p.event.Issue

	log.Printf("Processing issue #%d with action: %s", issue.GetNumber(), action)

	// Parse target branches from issue labels
	targetBranches := ghclient.ParseTargetBranches(issue.Labels, p.config.LabelPattern)

	if len(targetBranches) == 0 {
		log.Printf("No pronto labels found on issue #%d, skipping", issue.GetNumber())
		return nil
	}

	log.Printf("Found %d target branch(es): %v", len(targetBranches), getBranchNames(targetBranches))

	// Parse PR numbers from issue body
	prNumbers := ParsePRNumbers(issue.GetBody())
	if len(prNumbers) == 0 {
		log.Printf("No PR numbers found in issue #%d body, skipping", issue.GetNumber())
		return nil
	}

	log.Printf("Found %d PR number(s): %v", len(prNumbers), prNumbers)

	// Validate PRs exist and are merged
	validationResults := ValidatePRs(p.ctx, p.ghClient, prNumbers)
	mergedPRs := FilterMergedPRs(validationResults)

	if len(mergedPRs) == 0 {
		log.Printf("No merged PRs found in issue #%d", issue.GetNumber())
		// Post comment about validation failures
		if err := p.createValidationErrorComment(validationResults); err != nil {
			log.Printf("Failed to create validation error comment: %v", err)
		}
		return nil
	}

	log.Printf("Validated %d merged PR(s): %v", len(mergedPRs), mergedPRs)

	// If issue is being closed, handle tag creation only
	if action == EventActionClosed {
		return p.handleIssueClosed(targetBranches, mergedPRs)
	}

	// Check if we should skip processing - this prevents duplicate comments
	// when multiple issue events fire (opened, labeled, edited, etc.)
	// Only check this for non-closed issues
	shouldSkip, reason := p.shouldSkipProcessing(issue.GetNumber(), prNumbers, targetBranches)
	if shouldSkip {
		log.Printf("Skipping processing of issue #%d: %s", issue.GetNumber(), reason)
		return nil
	}

	// Get the user who triggered the action
	username := p.getUserForPermissionCheck(action)
	log.Printf("Checking permissions for user: %s", username)

	// Check if user has write permissions
	hasWriteAccess, err := p.permChecker.HasWriteAccess(p.ctx, username)
	if err != nil {
		return fmt.Errorf("failed to check permissions for user %s: %w", username, err)
	}

	log.Printf("User %s has write access: %t", username, hasWriteAccess)

	// Process PR×Branch matrix
	results := p.processMatrix(mergedPRs, targetBranches, hasWriteAccess)

	// Create status table comment
	if err := p.createTableComment(results); err != nil {
		log.Printf("Failed to create status table comment: %v", err)
	}

	return nil
}

// processMatrix processes all PR+branch combinations.
func (p *IssueProcessor) processMatrix(prNumbers []int, targetBranches []*models.TargetBranch, hasWriteAccess bool) []BatchResult {
	var results []BatchResult

	for _, prNum := range prNumbers {
		// Get PR details
		pr, err := p.ghClient.GetPullRequest(p.ctx, prNum)
		if err != nil {
			log.Printf("Failed to get PR #%d: %v", prNum, err)
			for _, branch := range targetBranches {
				results = append(results, BatchResult{
					PRNumber:     prNum,
					TargetBranch: branch.Name,
					Success:      false,
					Status:       "failed",
					Message:      fmt.Sprintf("Failed to fetch PR details: %v", err),
				})
			}
			continue
		}

		// Get commits from the PR
		commits, err := p.ghClient.GetPullRequestCommits(p.ctx, prNum)
		if err != nil {
			log.Printf("Failed to get commits for PR #%d: %v", prNum, err)
			for _, branch := range targetBranches {
				results = append(results, BatchResult{
					PRNumber:     prNum,
					TargetBranch: branch.Name,
					Success:      false,
					Status:       "failed",
					Message:      fmt.Sprintf("Failed to fetch commits: %v", err),
				})
			}
			continue
		}

		commitSHAs := make([]string, len(commits))
		commitMessages := make([]string, len(commits))
		for i, commit := range commits {
			commitSHAs[i] = commit.GetSHA()
			commitMessages[i] = fmt.Sprintf("%s (%s)", commit.Commit.GetMessage(), commit.GetSHA()[:7])
		}

		// Process each target branch for this PR
		for _, targetBranch := range targetBranches {
			result := p.processPRBranch(prNum, pr, targetBranch, commitSHAs, commitMessages, hasWriteAccess)
			results = append(results, result)
		}
	}

	return results
}

// processPRBranch processes a single PR+branch combination.
// It delegates the core logic to the cherrypick.Service.
func (p *IssueProcessor) processPRBranch(prNum int, pr *github.PullRequest, targetBranch *models.TargetBranch, commitSHAs, commitMessages []string, hasWriteAccess bool) BatchResult {
	branchName := targetBranch.Name

	// Check deduplication using issue number + PR number + branch + SHA
	issueNum := p.event.Issue.GetNumber()
	headSHA := pr.GetHead().GetSHA()
	trackerKey := fmt.Sprintf("issue-%d-pr-%d-%s-%s", issueNum, prNum, branchName, headSHA[:7])

	if !p.tracker.ShouldProcess(issueNum, trackerKey, headSHA) {
		log.Printf("Already processed issue #%d, PR #%d to branch %s at SHA %s, skipping",
			issueNum, prNum, branchName, headSHA[:7])
		return BatchResult{
			PRNumber:     prNum,
			TargetBranch: branchName,
			Success:      true,
			Status:       "skipped",
			Message:      "Already processed (skipped duplicate)",
		}
	}

	// Mark as processed
	defer p.tracker.MarkProcessed(issueNum, trackerKey, headSHA)

	// Build options for the cherry-pick service
	opts := cherrypick.CherryPickOptions{
		Owner:          p.event.Repo.GetOwner().GetLogin(),
		Repo:           p.event.Repo.GetName(),
		CloneURL:       p.event.Repo.GetCloneURL(),
		Token:          p.config.GitHubToken,
		PRNumber:       prNum,
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

	svc := cherrypick.NewService(p.ctx, p.ghClient)
	cpResult := svc.CherryPickToTarget(opts)

	return BatchResult{
		PRNumber:     cpResult.PRNumber,
		TargetBranch: cpResult.TargetBranch,
		Success:      cpResult.Success,
		Status:       cpResult.Status,
		Message:      cpResult.Message,
		CreatedPR:    cpResult.CreatedPR,
	}
}

// createTableComment creates a status table comment on the issue.
func (p *IssueProcessor) createTableComment(results []BatchResult) error {
	issue := p.event.Issue

	var sb strings.Builder
	sb.WriteString("## 🤖 PROnto Batch Summary\n\n")
	sb.WriteString(fmt.Sprintf("Processed %d PR(s) to %d unique branch(es):\n\n",
		countUniquePRs(results), countUniqueBranches(results)))

	// Create table
	sb.WriteString("| PR | Branch | Status | Details |\n")
	sb.WriteString("|----|--------|--------|---------|")

	var successCount, failedCount, skippedCount, pendingCount int

	var conflictCount int

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

		// Sanitize message for markdown table - remove newlines and pipes
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

	comment := sb.String()

	// Check if a status table comment already exists
	existingComment, err := p.ghClient.FindProntoComment(p.ctx, issue.GetNumber(), StatusTableMarker)
	if err != nil {
		log.Printf("Error finding existing status table comment: %v", err)
		return fmt.Errorf("failed to check for existing status table: %w", err)
	}

	if existingComment != nil {
		// Update existing comment
		log.Printf("Found existing status table comment #%d, updating it", existingComment.GetID())
		if err := p.ghClient.UpdateComment(p.ctx, existingComment.GetID(), comment); err != nil {
			return fmt.Errorf("failed to update status table comment: %w", err)
		}
		log.Printf("✅ Updated existing status table comment on issue #%d with %d results", issue.GetNumber(), len(results))
	} else {
		// Create new comment
		log.Printf("No existing status table found, creating new one")
		if err := p.ghClient.AddComment(p.ctx, issue.GetNumber(), comment); err != nil {
			return fmt.Errorf("failed to add status table comment: %w", err)
		}
		log.Printf("✅ Created new status table comment on issue #%d with %d results", issue.GetNumber(), len(results))
	}

	return nil
}

// createValidationErrorComment creates a comment explaining validation failures.
func (p *IssueProcessor) createValidationErrorComment(validationResults []PRValidationResult) error {
	issue := p.event.Issue

	var sb strings.Builder
	sb.WriteString("## ⚠️ PROnto Validation Errors\n\n")
	sb.WriteString("The following PRs could not be processed:\n\n")

	for _, vr := range validationResults {
		if vr.Error != nil {
			fmt.Fprintf(&sb, "- PR #%d: %v\n", vr.Number, vr.Error)
		} else if !vr.Exists {
			fmt.Fprintf(&sb, "- PR #%d: Does not exist\n", vr.Number)
		} else if !vr.Merged {
			fmt.Fprintf(&sb, "- PR #%d: Not merged (only merged PRs can be cherry-picked)\n", vr.Number)
		}
	}

	sb.WriteString("\n---\n")
	sb.WriteString("🤖 Automated by [PROnto](https://github.com/theakshaypant/pronto)")

	comment := sb.String()

	// Check if a validation error comment already exists
	existingComment, err := p.ghClient.FindProntoComment(p.ctx, issue.GetNumber(), ValidationErrorMarker)
	if err != nil {
		return fmt.Errorf("failed to check for existing validation error comment: %w", err)
	}

	if existingComment != nil {
		// Update existing comment
		if err := p.ghClient.UpdateComment(p.ctx, existingComment.GetID(), comment); err != nil {
			return fmt.Errorf("failed to update validation error comment: %w", err)
		}
		log.Printf("Updated existing validation error comment on issue #%d", issue.GetNumber())
	} else {
		// Create new comment
		if err := p.ghClient.AddComment(p.ctx, issue.GetNumber(), comment); err != nil {
			return fmt.Errorf("failed to add validation error comment: %w", err)
		}
		log.Printf("Added validation error comment to issue #%d", issue.GetNumber())
	}

	return nil
}

// shouldSkipProcessing determines if we should skip processing this issue.
// This prevents duplicate comments when multiple issue events fire in quick succession.
func (p *IssueProcessor) shouldSkipProcessing(issueNumber int, prNumbers []int, targetBranches []*models.TargetBranch) (bool, string) {
	// Check for existing status table
	existingTable, err := p.ghClient.FindProntoComment(p.ctx, issueNumber, StatusTableMarker)
	if err != nil {
		log.Printf("Error checking for existing status table: %v", err)
		return false, ""
	}

	// If no status table exists, we should process (might create validation errors)
	if existingTable == nil {
		log.Printf("No existing status table found, will process issue")
		return false, ""
	}

	// Status table exists - check if all PRs are already tracked
	tableBody := existingTable.GetBody()

	// Count how many PRs from the issue body are already in the table
	prsInTable := 0
	for _, prNum := range prNumbers {
		// Check if this PR appears in the table
		if strings.Contains(tableBody, fmt.Sprintf("[#%d](#%d)", prNum, prNum)) {
			prsInTable++
		}
	}

	// Count how many branches are in the table
	branchesInTable := 0
	for _, branch := range targetBranches {
		if strings.Contains(tableBody, fmt.Sprintf("`%s`", branch.Name)) {
			branchesInTable++
		}
	}

	// If all PRs and all branches are already in the table, skip processing
	if prsInTable == len(prNumbers) && branchesInTable == len(targetBranches) {
		log.Printf("All %d PRs and %d branches already in status table", prsInTable, branchesInTable)
		return true, "status table already contains all PRs and branches"
	}

	// Some PRs or branches are missing - should process to update
	log.Printf("Status table exists but missing some PRs (%d/%d) or branches (%d/%d), will process",
		prsInTable, len(prNumbers), branchesInTable, len(targetBranches))
	return false, ""
}

// handleIssueClosed handles tag creation when an issue is closed.
func (p *IssueProcessor) handleIssueClosed(targetBranches []*models.TargetBranch, mergedPRs []int) error {
	issue := p.event.Issue
	log.Printf("Issue #%d closed, checking for tag creation", issue.GetNumber())

	// Find branches with tag names specified
	var tagsToCreate []struct {
		branch string
		tag    string
	}

	for _, tb := range targetBranches {
		if tb.TagName != "" {
			tagsToCreate = append(tagsToCreate, struct {
				branch string
				tag    string
			}{branch: tb.Name, tag: tb.TagName})
		}
	}

	if len(tagsToCreate) == 0 {
		log.Printf("No tags specified in issue labels")
		return nil
	}

	log.Printf("Found %d tag(s) to create", len(tagsToCreate))

	var createdTags []string
	var failedTags []string

	for _, tagInfo := range tagsToCreate {
		log.Printf("Creating tag %s on branch %s", tagInfo.tag, tagInfo.branch)

		// Check if branch exists
		branchExists, err := p.ghClient.BranchExists(p.ctx, tagInfo.branch)
		if err != nil {
			log.Printf("Failed to check if branch %s exists: %v", tagInfo.branch, err)
			failedTags = append(failedTags, fmt.Sprintf("%s (branch check failed)", tagInfo.tag))
			continue
		}

		if !branchExists {
			log.Printf("Branch %s does not exist, skipping tag %s", tagInfo.branch, tagInfo.tag)
			failedTags = append(failedTags, fmt.Sprintf("%s (branch not found)", tagInfo.tag))
			continue
		}

		// Get current SHA of the target branch
		branchSHA, err := p.ghClient.GetBranchSHA(p.ctx, tagInfo.branch)
		if err != nil {
			log.Printf("Failed to get SHA for branch %s: %v", tagInfo.branch, err)
			failedTags = append(failedTags, fmt.Sprintf("%s (failed to get SHA)", tagInfo.tag))
			continue
		}

		// Create tag message
		tagMessage := fmt.Sprintf("Release tag for issue #%d\n\nBranch: %s\nPRs included: %v",
			issue.GetNumber(), tagInfo.branch, mergedPRs)

		// Create annotated tag
		if err := p.ghClient.CreateTag(p.ctx, tagInfo.tag, branchSHA, tagMessage); err != nil {
			log.Printf("Failed to create tag %s: %v", tagInfo.tag, err)
			failedTags = append(failedTags, fmt.Sprintf("%s (%v)", tagInfo.tag, err))
			continue
		}

		log.Printf("Successfully created tag %s on branch %s at SHA %s", tagInfo.tag, tagInfo.branch, branchSHA[:7])
		createdTags = append(createdTags, fmt.Sprintf("`%s` on `%s`", tagInfo.tag, tagInfo.branch))
	}

	// Post comment to issue about tag creation
	if len(createdTags) > 0 || len(failedTags) > 0 {
		var comment strings.Builder
		comment.WriteString("## 🏷️ Tag Creation Summary\n\n")

		if len(createdTags) > 0 {
			comment.WriteString("**✅ Tags created:**\n")
			for _, tag := range createdTags {
				comment.WriteString(fmt.Sprintf("- %s\n", tag))
			}
			comment.WriteString("\n")
		}

		if len(failedTags) > 0 {
			comment.WriteString("**❌ Failed to create:**\n")
			for _, tag := range failedTags {
				comment.WriteString(fmt.Sprintf("- %s\n", tag))
			}
			comment.WriteString("\n")
		}

		comment.WriteString("---\n")
		comment.WriteString("🤖 Automated by [PROnto](https://github.com/theakshaypant/pronto)")

		if err := p.ghClient.AddComment(p.ctx, issue.GetNumber(), comment.String()); err != nil {
			log.Printf("Failed to add tag creation comment: %v", err)
		}
	}

	return nil
}

// getUserForPermissionCheck determines which user to check permissions for.
func (p *IssueProcessor) getUserForPermissionCheck(action EventAction) string {
	switch action {
	case EventActionLabeled, EventActionOpened, EventActionEdited:
		if p.event.Sender != nil && p.event.Sender.Login != nil {
			return *p.event.Sender.Login
		}
	}

	// Fallback to issue author
	if p.event.Issue != nil && p.event.Issue.User != nil && p.event.Issue.User.Login != nil {
		return *p.event.Issue.User.Login
	}

	return ""
}

// SafeProcess wraps the Process method with panic recovery.
func (p *IssueProcessor) SafeProcess(action EventAction) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in IssueProcessor.Process: %v", r)
			err = fmt.Errorf("panic during processing: %v", r)
		}
	}()

	return p.Process(action)
}

// Helper functions

func validateIssueEvent(event *github.IssuesEvent) error {
	if event == nil {
		return fmt.Errorf("event cannot be nil")
	}
	if event.Issue == nil {
		return fmt.Errorf("issue cannot be nil")
	}
	if event.Repo == nil {
		return fmt.Errorf("repository cannot be nil")
	}
	return nil
}

func getBranchNames(branches []*models.TargetBranch) []string {
	names := make([]string, len(branches))
	for i, b := range branches {
		names[i] = b.Name
	}
	return names
}

func countUniquePRs(results []BatchResult) int {
	seen := make(map[int]bool)
	for _, r := range results {
		seen[r.PRNumber] = true
	}
	return len(seen)
}

func countUniqueBranches(results []BatchResult) int {
	seen := make(map[string]bool)
	for _, r := range results {
		seen[r.TargetBranch] = true
	}
	return len(seen)
}

// sanitizeTableCell removes characters that would break markdown table formatting.
func sanitizeTableCell(msg string) string {
	// Replace newlines with spaces
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")

	// Replace pipe characters with similar-looking character to avoid breaking table
	msg = strings.ReplaceAll(msg, "|", "ǀ")

	// Collapse multiple spaces into one
	msg = strings.Join(strings.Fields(msg), " ")

	// Trim to reasonable length for table display
	if len(msg) > 100 {
		msg = msg[:97] + "..."
	}

	return msg
}

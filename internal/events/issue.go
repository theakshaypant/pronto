package events

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v81/github"
	"github.com/theakshaypant/pronto/internal/action"
	"github.com/theakshaypant/pronto/internal/deduplication"
	"github.com/theakshaypant/pronto/internal/git"
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
func (p *IssueProcessor) processPRBranch(prNum int, pr *github.PullRequest, targetBranch *models.TargetBranch, commitSHAs, commitMessages []string, hasWriteAccess bool) BatchResult {
	branchName := targetBranch.Name

	result := BatchResult{
		PRNumber:     prNum,
		TargetBranch: branchName,
	}

	// Check deduplication using issue number + PR number + branch + SHA
	issueNum := p.event.Issue.GetNumber()
	headSHA := pr.GetHead().GetSHA()
	trackerKey := fmt.Sprintf("issue-%d-pr-%d-%s-%s", issueNum, prNum, branchName, headSHA[:7])

	if !p.tracker.ShouldProcess(issueNum, trackerKey, headSHA) {
		log.Printf("Already processed issue #%d, PR #%d to branch %s at SHA %s, skipping",
			issueNum, prNum, branchName, headSHA[:7])
		result.Success = true
		result.Status = "skipped"
		result.Message = "Already processed (skipped duplicate)"
		return result
	}

	// Mark as processed
	defer p.tracker.MarkProcessed(issueNum, trackerKey, headSHA)

	// Check if target branch exists
	exists, err := p.ghClient.BranchExists(p.ctx, branchName)
	if err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed to check if branch exists: %v", err)
		return result
	}

	if !exists {
		result.Success = false
		result.Status = "failed"
		result.Message = "Branch does not exist"
		return result
	}

	// Create temporary directory for git operations
	tempDir, err := os.MkdirTemp("", "pronto-*")
	if err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed to create temp directory: %v", err)
		return result
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "repo")

	// Clone repository
	log.Printf("Cloning repository for PR #%d to %s", prNum, branchName)
	repo, err := git.Clone(git.CloneOptions{
		URL:       p.event.Repo.GetCloneURL(),
		Token:     p.config.GitHubToken,
		Directory: repoDir,
		Depth:     0,
	})
	if err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed to clone repository: %v", err)
		return result
	}

	// Configure git user
	if err := repo.ConfigUser(p.config.BotName, p.config.BotEmail); err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed to configure git user: %v", err)
		return result
	}

	// Checkout target branch
	log.Printf("Checking out target branch: %s", branchName)
	if err := repo.Checkout(branchName); err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed to checkout target branch: %v", err)
		return result
	}

	// Perform cherry-pick
	log.Printf("Cherry-picking %d commit(s) from PR #%d", len(commitSHAs), prNum)
	cherryPickResult, err := repo.CherryPick(commitSHAs...)
	if err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Cherry-pick operation failed: %v", err)
		return result
	}

	// Handle cherry-pick result
	if !cherryPickResult.Success {
		// Conflicts detected - create a PR for manual resolution
		log.Printf("Cherry-pick has conflicts, creating conflict PR")
		return p.createConflictPR(repo, prNum, branchName, commitMessages, cherryPickResult)
	}

	// Cherry-pick succeeded - push or create PR
	if hasWriteAccess && !p.config.AlwaysCreatePR {
		// Push directly
		log.Printf("Pushing cherry-picked commits to %s", branchName)
		if err := repo.Push("origin", branchName, false); err != nil {
			result.Success = false
			result.Status = "failed"
			result.Message = fmt.Sprintf("Failed to push to target branch: %v", err)
			return result
		}

		result.Success = true
		result.Status = "success"
		result.Message = fmt.Sprintf("Cherry-picked %d commit(s)", len(commitMessages))
		return result
	}

	// Create cherry-pick PR
	reason := "user lacks write access"
	if p.config.AlwaysCreatePR {
		reason = "always_create_pr is enabled"
	}
	log.Printf("Creating cherry-pick PR (%s)", reason)

	cherryPickBranch := fmt.Sprintf("pronto/%s/pr-%d", branchName, prNum)
	if err := repo.CreateBranch(cherryPickBranch); err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed to create cherry-pick branch: %v", err)
		return result
	}

	if err := repo.Push("origin", cherryPickBranch, false); err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed to push cherry-pick branch: %v", err)
		return result
	}

	// Create PR
	prBody := ghclient.FormatConflictPRBody(prNum, branchName, commitMessages, "", "")
	prTitle := fmt.Sprintf("[PROnto] Cherry-pick PR #%d to %s", prNum, branchName)

	newPR, err := p.ghClient.CreatePullRequest(p.ctx, ghclient.PROptions{
		Title:  prTitle,
		Body:   prBody,
		Head:   cherryPickBranch,
		Base:   branchName,
		Labels: []string{"pronto"},
	})
	if err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed to create cherry-pick PR: %v", err)
		return result
	}

	log.Printf("Created cherry-pick PR #%d", newPR.GetNumber())

	result.Success = true
	result.Status = "pending_pr"
	result.Message = "Created cherry-pick PR"
	result.CreatedPR = newPR.GetNumber()
	return result
}

// createConflictPR creates a PR with conflicted cherry-pick for manual resolution.
func (p *IssueProcessor) createConflictPR(repo *git.Repository, prNum int, targetBranch string, commitMessages []string, cherryPickResult *git.CherryPickResult) BatchResult {
	result := BatchResult{
		PRNumber:     prNum,
		TargetBranch: targetBranch,
	}

	// Get conflict details
	details, err := repo.GetConflictDetails()
	if err != nil {
		log.Printf("Failed to get conflict details: %v", err)
		details = fmt.Sprintf("Conflicts in %d file(s)", len(cherryPickResult.ConflictedFiles))
	}

	// Create a new branch for the conflict PR
	conflictBranch := fmt.Sprintf("pronto/%s/pr-%d-conflicts", targetBranch, prNum)
	log.Printf("Creating conflict branch: %s", conflictBranch)

	if err := repo.CreateBranch(conflictBranch); err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed to create conflict branch: %v", err)
		return result
	}

	// Stage all files (including conflicts)
	if err := repo.StageAll(); err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed to stage conflicted files: %v", err)
		return result
	}

	// Commit the conflicts
	commitMsg := fmt.Sprintf("Cherry-pick PR #%d to %s (conflicts)\n\nConflicts need manual resolution", prNum, targetBranch)
	if err := repo.Commit(commitMsg); err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed to commit conflicts: %v", err)
		return result
	}

	// Push the branch
	log.Printf("Pushing conflict branch to origin")
	if err := repo.Push("origin", conflictBranch, false); err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed to push conflict branch: %v", err)
		return result
	}

	// Create PR with conflict label
	prBody := ghclient.FormatConflictPRBody(prNum, targetBranch, commitMessages, details, "")
	prTitle := fmt.Sprintf("[PROnto] Cherry-pick PR #%d to %s (CONFLICTS)", prNum, targetBranch)

	labels := []string{"pronto", p.config.ConflictLabel}

	newPR, err := p.ghClient.CreatePullRequest(p.ctx, ghclient.PROptions{
		Title:  prTitle,
		Body:   prBody,
		Head:   conflictBranch,
		Base:   targetBranch,
		Labels: labels,
	})
	if err != nil {
		result.Success = false
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed to create conflict PR: %v", err)
		return result
	}

	log.Printf("Created conflict PR #%d", newPR.GetNumber())

	result.Success = false // Mark as failed since manual resolution is needed
	result.Status = "conflict"
	result.Message = fmt.Sprintf("Conflicts - manual resolution needed")
	result.CreatedPR = newPR.GetNumber()
	return result
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
			if r.Status == "conflict" {
				msg = fmt.Sprintf("Conflicts - see [PR #%d](#%d)", r.CreatedPR, r.CreatedPR)
			} else if r.Status == "pending_pr" {
				msg = fmt.Sprintf("Pending merge of [PR #%d](#%d)", r.CreatedPR, r.CreatedPR)
			}
		}

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

	if err := p.ghClient.AddComment(p.ctx, issue.GetNumber(), comment); err != nil {
		return fmt.Errorf("failed to add status table comment: %w", err)
	}

	log.Printf("Added status table comment to issue #%d with %d results", issue.GetNumber(), len(results))
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

	return p.ghClient.AddComment(p.ctx, issue.GetNumber(), sb.String())
}

// handleIssueClosed handles tag creation when an issue is closed.
func (p *IssueProcessor) handleIssueClosed(targetBranches []*models.TargetBranch, mergedPRs []int) error {
	log.Printf("Issue closed, checking for tag creation")

	// TODO: Implement tag creation logic

	log.Printf("Tag creation on issue close not yet implemented")
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

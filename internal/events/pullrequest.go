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
func (p *PRProcessor) processTargetBranch(target *models.TargetBranch, commitSHAs, commitMessages []string, hasWriteAccess bool) ProcessResult {
	pr := p.event.PullRequest
	targetBranch := target.Name

	// Check if target branch exists
	exists, err := p.ghClient.BranchExists(p.ctx, targetBranch)
	if err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to check if branch exists: %v", err),
		}
	}

	// Handle branch creation if specified with .. notation
	if !exists && target.ShouldCreate {
		log.Printf("Branch %s does not exist, creating from %s", targetBranch, target.BaseBranch)

		// Verify base branch exists
		baseExists, err := p.ghClient.BranchExists(p.ctx, target.BaseBranch)
		if err != nil {
			return ProcessResult{
				TargetBranch: targetBranch,
				Success:      false,
				Message:      fmt.Sprintf("❌ Failed to check if base branch `%s` exists: %v", target.BaseBranch, err),
			}
		}

		if !baseExists {
			return ProcessResult{
				TargetBranch: targetBranch,
				Success:      false,
				Message:      fmt.Sprintf("⚠️ Cannot create branch because the base branch `%s` does not exist. Please check the label format or create the base branch first.", target.BaseBranch),
			}
		}

		// Get base branch SHA
		baseSHA, err := p.ghClient.GetBranchSHA(p.ctx, target.BaseBranch)
		if err != nil {
			return ProcessResult{
				TargetBranch: targetBranch,
				Success:      false,
				Message:      fmt.Sprintf("❌ Failed to get base branch SHA: %v", err),
			}
		}

		// Create the target branch
		if err := p.ghClient.CreateBranch(p.ctx, targetBranch, baseSHA); err != nil {
			return ProcessResult{
				TargetBranch: targetBranch,
				Success:      false,
				Message:      fmt.Sprintf("❌ Failed to create target branch: %v", err),
			}
		}

		log.Printf("Successfully created branch %s from %s", targetBranch, target.BaseBranch)
		// Continue processing to cherry-pick to the newly created branch
	} else if !exists {
		// Branch doesn't exist and no .. notation specified
		log.Printf("Branch %s does not exist", targetBranch)
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("⚠️ Branch does not exist. Use label `%s%s..base-branch` to create it automatically.", p.config.LabelPattern, targetBranch),
		}
	}

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

	// Create temporary directory for git operations
	tempDir, err := os.MkdirTemp("", "pronto-*")
	if err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to create temp directory: %v", err),
		}
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "repo")

	// Clone repository
	log.Printf("Cloning repository to %s", repoDir)
	repo, err := git.Clone(git.CloneOptions{
		URL:       p.event.Repo.GetCloneURL(),
		Token:     p.config.GitHubToken,
		Directory: repoDir,
		Depth:     0, // Full clone needed for cherry-pick
	})
	if err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to clone repository: %v", err),
		}
	}

	// Configure git user
	if err := repo.ConfigUser(p.config.BotName, p.config.BotEmail); err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to configure git user: %v", err),
		}
	}

	// Checkout target branch
	log.Printf("Checking out target branch: %s", targetBranch)
	if err := repo.Checkout(targetBranch); err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to checkout target branch: %v", err),
		}
	}

	// Perform cherry-pick
	log.Printf("Cherry-picking %d commit(s)", len(commitSHAs))
	result, err := repo.CherryPick(commitSHAs...)
	if err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Cherry-pick operation failed: %v", err),
		}
	}

	// Handle result
	if result.Success {
		return p.handleSuccessfulCherryPick(repo, target, commitMessages, hasWriteAccess)
	}

	return p.handleConflictedCherryPick(repo, targetBranch, commitMessages, result, pr.GetNumber(), target.TagName)
}

// handleSuccessfulCherryPick handles a successful cherry-pick.
func (p *PRProcessor) handleSuccessfulCherryPick(repo *git.Repository, target *models.TargetBranch, commitMessages []string, hasWriteAccess bool) ProcessResult {
	pr := p.event.PullRequest
	targetBranch := target.Name

	if hasWriteAccess && !p.config.AlwaysCreatePR {
		// User has write access and always_create_pr is not enabled - push directly
		log.Printf("Pushing cherry-picked commits to %s", targetBranch)
		if err := repo.Push("origin", targetBranch, false); err != nil {
			return ProcessResult{
				TargetBranch: targetBranch,
				Success:      false,
				Message:      fmt.Sprintf("❌ Failed to push to target branch: %s", git.SanitizeError(err)),
			}
		}

		log.Printf("Successfully cherry-picked to %s", targetBranch)

		// Create tag if specified
		if target.TagName != "" {
			log.Printf("Creating tag %s for branch %s", target.TagName, targetBranch)

			tagMessage := fmt.Sprintf("Cherry-picked PR #%d to %s\n\nOriginal PR: #%d\nCommits: %d",
				pr.GetNumber(), targetBranch, pr.GetNumber(), len(commitMessages))

			if err := repo.CreateTag(target.TagName, tagMessage); err != nil {
				// Tag creation failed - log but don't fail the whole operation
				log.Printf("Failed to create tag %s: %v", target.TagName, err)
				return ProcessResult{
					TargetBranch: targetBranch,
					Success:      false,
					Message:      fmt.Sprintf("❌ Cherry-pick succeeded but tag creation failed: %v", err),
				}
			}

			// Push the tag
			if err := repo.PushTag("origin", target.TagName); err != nil {
				log.Printf("Failed to push tag %s: %v", target.TagName, git.SanitizeError(err))
				return ProcessResult{
					TargetBranch: targetBranch,
					Success:      false,
					Message:      fmt.Sprintf("❌ Cherry-pick and tag creation succeeded but tag push failed: %s", git.SanitizeError(err)),
				}
			}

			log.Printf("Successfully created and pushed tag %s", target.TagName)
		}

		// Build success message
		msg := fmt.Sprintf("✅ Successfully cherry-picked %d commit(s)", len(commitMessages))
		if target.ShouldCreate {
			msg = fmt.Sprintf("✅ Created branch from `%s` and cherry-picked %d commit(s)", target.BaseBranch, len(commitMessages))
		}
		if target.TagName != "" {
			msg += fmt.Sprintf(", created tag `%s`", target.TagName)
		}

		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      true,
			Message:      msg,
		}
	}

	// Create PR (either user lacks write access or always_create_pr is enabled)
	reason := "user lacks write access"
	if p.config.AlwaysCreatePR {
		reason = "always_create_pr is enabled"
	}
	log.Printf("Creating PR (%s)", reason)
	return p.createCherryPickPR(repo, target, commitMessages, "", pr.GetNumber())
}

// handleConflictedCherryPick handles a cherry-pick with conflicts.
func (p *PRProcessor) handleConflictedCherryPick(repo *git.Repository, targetBranch string, commitMessages []string, result *git.CherryPickResult, prNumber int, tagName string) ProcessResult {
	log.Printf("Cherry-pick resulted in conflicts for branch %s", targetBranch)

	// Get conflict details while we still have the conflicts in working tree
	details, err := repo.GetConflictDetails()
	if err != nil {
		log.Printf("Failed to get conflict details: %v", err)
		details = fmt.Sprintf("Conflicts in %d file(s)", len(result.ConflictedFiles))
	}

	// Stage all files (including conflicts with markers)
	// This must be done while still in the conflicted cherry-pick state
	if err := repo.StageAll(); err != nil {
		// Abort cherry-pick before returning
		repo.AbortCherryPick()
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to stage conflicted files: %v", err),
		}
	}

	// Commit the conflicts to complete the cherry-pick
	// This creates a commit with conflict markers that can be resolved later
	commitMsg := fmt.Sprintf("Cherry-pick PR #%d to %s (conflicts)\n\nConflicts need manual resolution", prNumber, targetBranch)
	if err := repo.Commit(commitMsg); err != nil {
		// Abort cherry-pick before returning
		repo.AbortCherryPick()
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to commit conflicts: %v", err),
		}
	}

	// Now create a new branch from this commit
	conflictBranch := fmt.Sprintf("pronto/%s/pr-%d-conflicts", targetBranch, prNumber)
	log.Printf("Creating conflict branch: %s", conflictBranch)

	if err := repo.CreateBranch(conflictBranch); err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to create conflict branch: %v", err),
		}
	}

	// Push the conflict branch (force push in case it exists from a previous attempt)
	log.Printf("Pushing conflict branch to origin (force)")
	if err := repo.Push("origin", conflictBranch, true); err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to push conflict branch: %s", git.SanitizeError(err)),
		}
	}

	// Checkout back to the base branch and reset it to remove the conflict commit
	// We want the base branch clean, with only the conflict branch having the markers
	if err := repo.Checkout(targetBranch); err != nil {
		log.Printf("Warning: Failed to checkout back to %s: %v", targetBranch, err)
	} else {
		// Reset to HEAD~1 to remove the conflict commit we just created
		if err := repo.ResetHard("HEAD~1"); err != nil {
			log.Printf("Warning: Failed to reset %s: %v", targetBranch, err)
		}
	}

	// Create PR with conflict label
	prBody := ghclient.FormatConflictPRBody(prNumber, targetBranch, commitMessages, details, tagName)
	prTitle := fmt.Sprintf("[PROnto] Cherry-pick PR #%d to %s (CONFLICTS)", prNumber, targetBranch)

	labels := []string{"pronto", p.config.ConflictLabel}

	newPR, err := p.ghClient.CreatePullRequest(p.ctx, ghclient.PROptions{
		Title:  prTitle,
		Body:   prBody,
		Head:   conflictBranch,
		Base:   targetBranch,
		Labels: labels,
	})
	if err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to create conflict PR: %v", err),
		}
	}

	log.Printf("Created conflict PR #%d", newPR.GetNumber())

	return ProcessResult{
		TargetBranch: targetBranch,
		Success:      false,
		Message:      fmt.Sprintf("⚠️ Conflicts detected - created PR #%d for manual resolution", newPR.GetNumber()),
	}
}

// createCherryPickPR creates a PR for cherry-picked changes.
func (p *PRProcessor) createCherryPickPR(repo *git.Repository, target *models.TargetBranch, commitMessages []string, conflictDetails string, prNumber int) ProcessResult {
	targetBranch := target.Name
	// Create a new branch for the cherry-pick PR
	branchName := fmt.Sprintf("pronto/%s/pr-%d", targetBranch, prNumber)

	log.Printf("Creating cherry-pick branch: %s", branchName)
	if err := repo.CreateBranch(branchName); err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to create cherry-pick branch: %v", err),
		}
	}

	// Push the branch
	log.Printf("Pushing cherry-pick branch to origin")
	if err := repo.Push("origin", branchName, false); err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to push cherry-pick branch: %s", git.SanitizeError(err)),
		}
	}

	// Create PR
	prBody := ghclient.FormatConflictPRBody(prNumber, targetBranch, commitMessages, conflictDetails, target.TagName)
	prTitle := fmt.Sprintf("[PROnto] Cherry-pick PR #%d to %s", prNumber, targetBranch)

	// Always add "pronto" label to identify auto-created PRs
	labels := []string{"pronto"}
	if conflictDetails != "" {
		labels = append(labels, p.config.ConflictLabel)
	}

	newPR, err := p.ghClient.CreatePullRequest(p.ctx, ghclient.PROptions{
		Title:  prTitle,
		Body:   prBody,
		Head:   branchName,
		Base:   targetBranch,
		Labels: labels,
	})
	if err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to create cherry-pick PR: %v", err),
		}
	}

	log.Printf("Created cherry-pick PR #%d", newPR.GetNumber())

	// Build success message
	msg := fmt.Sprintf("✅ Created PR #%d", newPR.GetNumber())
	if target.ShouldCreate {
		msg = fmt.Sprintf("✅ Created branch from `%s` and created PR #%d", target.BaseBranch, newPR.GetNumber())
	}
	if target.TagName != "" {
		msg += fmt.Sprintf(" (tag `%s` pending merge)", target.TagName)
	}

	return ProcessResult{
		TargetBranch: targetBranch,
		Success:      true,
		Message:      msg,
		CreatedPR:    newPR.GetNumber(),
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

	// Process each target branch
	var results []BatchResult
	for _, targetBranch := range targetBranches {
		log.Printf("Processing target branch: %s for PR #%d", targetBranch.Name, prNumber)

		result := BatchResult{
			PRNumber:     prNumber,
			TargetBranch: targetBranch.Name,
		}

		// Check deduplication
		if p.tracker.IsProcessed(prNumber, targetBranch.Name, pr.GetHead().GetSHA()) {
			log.Printf("Skipping duplicate processing for PR #%d to %s", prNumber, targetBranch.Name)
			result.Success = false
			result.Status = "skipped"
			result.Message = "Already processed (duplicate)"
			results = append(results, result)
			continue
		}

		// Mark as processed
		p.tracker.MarkProcessed(prNumber, targetBranch.Name, pr.GetHead().GetSHA())

		// Create temporary directory for git operations
		tempDir, err := os.MkdirTemp("", "pronto-*")
		if err != nil {
			result.Success = false
			result.Status = "failed"
			result.Message = fmt.Sprintf("Failed to create temp directory: %v", err)
			results = append(results, result)
			continue
		}

		repoDir := filepath.Join(tempDir, "repo")

		// Clone repository
		log.Printf("Cloning repository to %s", repoDir)
		repo, err := git.Clone(git.CloneOptions{
			URL:       p.event.Repo.GetCloneURL(),
			Token:     p.config.GitHubToken,
			Directory: repoDir,
			Depth:     0, // Full clone needed for cherry-pick
		})
		if err != nil {
			os.RemoveAll(tempDir)
			result.Success = false
			result.Status = "failed"
			result.Message = fmt.Sprintf("Failed to clone repository: %v", git.SanitizeError(err))
			results = append(results, result)
			continue
		}

		// Configure git user
		if err := repo.ConfigUser(p.config.BotName, p.config.BotEmail); err != nil {
			os.RemoveAll(tempDir)
			result.Success = false
			result.Status = "failed"
			result.Message = fmt.Sprintf("Failed to configure git: %v", err)
			results = append(results, result)
			continue
		}

		// Check if target branch exists
		branchExists, err := p.ghClient.BranchExists(p.ctx, targetBranch.Name)
		if err != nil {
			os.RemoveAll(tempDir)
			result.Success = false
			result.Status = "failed"
			result.Message = fmt.Sprintf("Failed to check branch existence: %v", err)
			results = append(results, result)
			continue
		}

		if !branchExists {
			os.RemoveAll(tempDir)
			result.Success = false
			result.Status = "failed"
			result.Message = fmt.Sprintf("Target branch '%s' does not exist", targetBranch.Name)
			results = append(results, result)
			continue
		}

		// Checkout target branch
		if err := repo.Checkout(targetBranch.Name); err != nil {
			os.RemoveAll(tempDir)
			result.Success = false
			result.Status = "failed"
			result.Message = fmt.Sprintf("Failed to checkout branch: %v", err)
			results = append(results, result)
			continue
		}

		// Perform cherry-pick
		cherryPickResult, err := repo.CherryPick(commitSHAs...)
		if err != nil {
			os.RemoveAll(tempDir)
			result.Success = false
			result.Status = "failed"
			result.Message = fmt.Sprintf("Cherry-pick failed: %v", err)
			results = append(results, result)
			continue
		}

		if !cherryPickResult.Success {
			os.RemoveAll(tempDir)
			// Conflicts - for now just report
			result.Success = false
			result.Status = "conflict"
			result.Message = fmt.Sprintf("Conflicts in %d file(s) - manual cherry-pick needed", len(cherryPickResult.ConflictedFiles))
			results = append(results, result)
			continue
		}

		// Push changes or create PR
		if hasWriteAccess && !p.config.AlwaysCreatePR {
			// Push directly
			if err := repo.Push("origin", targetBranch.Name, false); err != nil {
				os.RemoveAll(tempDir)
				result.Success = false
				result.Status = "failed"
				result.Message = fmt.Sprintf("Failed to push: %s", git.SanitizeError(err))
				results = append(results, result)
				continue
			}

			result.Success = true
			result.Status = "success"
			result.Message = fmt.Sprintf("Cherry-picked %d commit(s)", len(commitMessages))
		} else {
			// Create PR
			branchName := fmt.Sprintf("pronto/%s/pr-%d", targetBranch.Name, prNumber)
			if err := repo.CreateBranch(branchName); err != nil {
				os.RemoveAll(tempDir)
				result.Success = false
				result.Status = "failed"
				result.Message = fmt.Sprintf("Failed to create branch: %v", err)
				results = append(results, result)
				continue
			}

			if err := repo.Push("origin", branchName, false); err != nil {
				os.RemoveAll(tempDir)
				result.Success = false
				result.Status = "failed"
				result.Message = fmt.Sprintf("Failed to push: %s", git.SanitizeError(err))
				results = append(results, result)
				continue
			}

			// Create the PR
			prBody := ghclient.FormatConflictPRBody(prNumber, targetBranch.Name, commitMessages, "", "")
			prTitle := fmt.Sprintf("[PROnto] Cherry-pick PR #%d to %s", prNumber, targetBranch.Name)

			newPR, err := p.ghClient.CreatePullRequest(p.ctx, ghclient.PROptions{
				Title:  prTitle,
				Body:   prBody,
				Head:   branchName,
				Base:   targetBranch.Name,
				Labels: []string{"pronto"},
			})
			if err != nil {
				os.RemoveAll(tempDir)
				result.Success = false
				result.Status = "failed"
				result.Message = fmt.Sprintf("Failed to create PR: %v", err)
				results = append(results, result)
				continue
			}

			result.Success = false // Pending PR merge
			result.Status = "pending_pr"
			result.Message = "Cherry-pick PR created"
			result.CreatedPR = newPR.GetNumber()
		}

		// Clean up
		os.RemoveAll(tempDir)
		results = append(results, result)
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

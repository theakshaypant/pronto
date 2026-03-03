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
	ghclient "github.com/theakshaypant/pronto/internal/github"
	"github.com/theakshaypant/pronto/internal/git"
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

	if len(targetBranches) == 0 {
		log.Printf("No pronto labels found on PR #%d, skipping", pr.GetNumber())
		return nil
	}

	log.Printf("Found %d target branch(es): %v", len(targetBranches), targetBranches)

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
		return p.handleSuccessfulCherryPick(repo, targetBranch, commitMessages, hasWriteAccess, target.ShouldCreate, target.BaseBranch)
	}

	return p.handleConflictedCherryPick(repo, targetBranch, commitMessages, result)
}

// handleSuccessfulCherryPick handles a successful cherry-pick.
func (p *PRProcessor) handleSuccessfulCherryPick(repo *git.Repository, targetBranch string, commitMessages []string, hasWriteAccess bool, branchCreated bool, baseBranch string) ProcessResult {
	pr := p.event.PullRequest

	if hasWriteAccess {
		// User has write access - push directly
		log.Printf("Pushing cherry-picked commits to %s", targetBranch)
		if err := repo.Push("origin", targetBranch, false); err != nil {
			return ProcessResult{
				TargetBranch: targetBranch,
				Success:      false,
				Message:      fmt.Sprintf("❌ Failed to push to target branch: %v", err),
			}
		}

		log.Printf("Successfully cherry-picked to %s", targetBranch)

		msg := fmt.Sprintf("✅ Successfully cherry-picked %d commit(s)", len(commitMessages))
		if branchCreated {
			msg = fmt.Sprintf("✅ Created branch from `%s` and cherry-picked %d commit(s)", baseBranch, len(commitMessages))
		}

		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      true,
			Message:      msg,
		}
	}

	// User lacks write access - create fallback PR
	log.Printf("User lacks write access, creating fallback PR")
	return p.createFallbackPR(repo, targetBranch, commitMessages, "", pr.GetNumber())
}

// handleConflictedCherryPick handles a cherry-pick with conflicts.
func (p *PRProcessor) handleConflictedCherryPick(repo *git.Repository, targetBranch string, commitMessages []string, result *git.CherryPickResult) ProcessResult {
	log.Printf("Cherry-pick resulted in conflicts for branch %s", targetBranch)

	// Get conflict details
	details, err := repo.GetConflictDetails()
	if err != nil {
		log.Printf("Failed to get conflict details: %v", err)
		details = fmt.Sprintf("Conflicts in %d file(s)", len(result.ConflictedFiles))
	}

	// Extract commit SHAs for the instructions
	cherryPickSHAs := extractCommitSHAsFromMessages(commitMessages)
	cherryPickCmd := "git cherry-pick " + strings.Join(cherryPickSHAs, " ")

	msg := fmt.Sprintf("⚠️ Cherry-pick resulted in conflicts. Manual resolution required.\n\n"+
		"**Conflict Details:**\n```\n%s\n```\n\n"+
		"**Commands to resolve:**\n```bash\n"+
		"git checkout %s\n"+
		"git pull origin %s\n"+
		"%s\n"+
		"# Resolve conflicts, then:\n"+
		"git add .\n"+
		"git cherry-pick --continue\n"+
		"git push origin %s\n"+
		"```",
		details,
		targetBranch,
		targetBranch,
		cherryPickCmd,
		targetBranch,
	)

	return ProcessResult{
		TargetBranch: targetBranch,
		Success:      false,
		Message:      msg,
	}
}

// createFallbackPR creates a PR for users without write access.
func (p *PRProcessor) createFallbackPR(repo *git.Repository, targetBranch string, commitMessages []string, conflictDetails string, prNumber int) ProcessResult {
	// Create a new branch for the fallback PR
	branchName := fmt.Sprintf("pronto/%s/pr-%d", targetBranch, prNumber)

	log.Printf("Creating fallback branch: %s", branchName)
	if err := repo.CreateBranch(branchName); err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to create fallback branch: %v", err),
		}
	}

	// Push the branch
	log.Printf("Pushing fallback branch to origin")
	if err := repo.Push("origin", branchName, false); err != nil {
		return ProcessResult{
			TargetBranch: targetBranch,
			Success:      false,
			Message:      fmt.Sprintf("❌ Failed to push fallback branch: %v", err),
		}
	}

	// Create PR
	prBody := ghclient.FormatConflictPRBody(prNumber, targetBranch, commitMessages, conflictDetails)
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
			Message:      fmt.Sprintf("❌ Failed to create fallback PR: %v", err),
		}
	}

	log.Printf("Created fallback PR #%d", newPR.GetNumber())

	return ProcessResult{
		TargetBranch: targetBranch,
		Success:      true,
		Message:      fmt.Sprintf("✅ Created PR #%d (no write access)", newPR.GetNumber()),
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

// extractCommitSHAsFromMessages extracts commit SHAs from formatted commit messages.
// Messages are in format: "Message (abc1234)"
func extractCommitSHAsFromMessages(messages []string) []string {
	var shas []string
	for _, msg := range messages {
		// Find SHA in parentheses at the end
		if idx := strings.LastIndex(msg, "("); idx != -1 {
			if endIdx := strings.LastIndex(msg, ")"); endIdx > idx {
				sha := msg[idx+1 : endIdx]
				shas = append(shas, sha)
			}
		}
	}
	return shas
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


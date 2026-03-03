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

	// Process each target branch
	for _, targetBranch := range targetBranches {
		log.Printf("Processing target branch: %s", targetBranch)

		if err := p.processTargetBranch(targetBranch, commitSHAs, commitMessages, hasWriteAccess); err != nil {
			log.Printf("Error processing branch %s: %v", targetBranch, err)
			// Continue with other branches even if one fails
		}
	}

	return nil
}

// processTargetBranch handles cherry-picking to a single target branch.
func (p *PRProcessor) processTargetBranch(targetBranch string, commitSHAs, commitMessages []string, hasWriteAccess bool) error {
	pr := p.event.PullRequest

	// Check if target branch exists
	exists, err := p.ghClient.BranchExists(p.ctx, targetBranch)
	if err != nil {
		return fmt.Errorf("failed to check if branch exists: %w", err)
	}

	if !exists {
		log.Printf("Branch %s does not exist, adding comment to PR", targetBranch)
		comment := ghclient.FormatMissingBranchComment(targetBranch, p.config.LabelPattern)
		if err := p.ghClient.AddComment(p.ctx, pr.GetNumber(), comment); err != nil {
			log.Printf("Failed to add missing branch comment: %v", err)
		}
		return nil
	}

	// Check deduplication - prevent processing the same PR/branch/SHA combination
	headSHA := pr.GetHead().GetSHA()
	if !p.tracker.ShouldProcess(pr.GetNumber(), targetBranch, headSHA) {
		log.Printf("Already processed PR #%d to branch %s at SHA %s, skipping", pr.GetNumber(), targetBranch, headSHA[:7])
		return nil
	}

	// Mark as processed to prevent duplicate processing from webhook retries
	defer p.tracker.MarkProcessed(pr.GetNumber(), targetBranch, headSHA)

	// Create temporary directory for git operations
	tempDir, err := os.MkdirTemp("", "pronto-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
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
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	// Configure git user
	if err := repo.ConfigUser(p.config.BotName, p.config.BotEmail); err != nil {
		return fmt.Errorf("failed to configure git user: %w", err)
	}

	// Checkout target branch
	log.Printf("Checking out target branch: %s", targetBranch)
	if err := repo.Checkout(targetBranch); err != nil {
		return fmt.Errorf("failed to checkout target branch: %w", err)
	}

	// Perform cherry-pick
	log.Printf("Cherry-picking %d commit(s)", len(commitSHAs))
	result, err := repo.CherryPick(commitSHAs...)
	if err != nil {
		return fmt.Errorf("cherry-pick operation failed: %w", err)
	}

	// Handle result
	if result.Success {
		return p.handleSuccessfulCherryPick(repo, targetBranch, commitMessages, hasWriteAccess)
	}

	return p.handleConflictedCherryPick(repo, targetBranch, commitSHAs, commitMessages, result)
}

// handleSuccessfulCherryPick handles a successful cherry-pick.
func (p *PRProcessor) handleSuccessfulCherryPick(repo *git.Repository, targetBranch string, commitMessages []string, hasWriteAccess bool) error {
	pr := p.event.PullRequest

	if hasWriteAccess {
		// User has write access - push directly
		log.Printf("Pushing cherry-picked commits to %s", targetBranch)
		if err := repo.Push("origin", targetBranch, false); err != nil {
			return fmt.Errorf("failed to push to target branch: %w", err)
		}

		// Add success comment
		comment := ghclient.FormatSuccessComment(targetBranch, commitMessages)
		if err := p.ghClient.AddComment(p.ctx, pr.GetNumber(), comment); err != nil {
			log.Printf("Failed to add success comment: %v", err)
		}

		log.Printf("Successfully cherry-picked to %s", targetBranch)
		return nil
	}

	// User lacks write access - create fallback PR
	log.Printf("User lacks write access, creating fallback PR")
	return p.createFallbackPR(repo, targetBranch, commitMessages, "")
}

// handleConflictedCherryPick handles a cherry-pick with conflicts.
func (p *PRProcessor) handleConflictedCherryPick(repo *git.Repository, targetBranch string, commitSHAs, commitMessages []string, result *git.CherryPickResult) error {
	log.Printf("Cherry-pick resulted in conflicts for branch %s", targetBranch)

	// Get conflict details
	details, err := repo.GetConflictDetails()
	if err != nil {
		log.Printf("Failed to get conflict details: %v", err)
		details = fmt.Sprintf("Conflicts in %d file(s)", len(result.ConflictedFiles))
	}

	// Create conflict PR
	return p.createConflictPR(targetBranch, commitMessages, details)
}

// createFallbackPR creates a PR for users without write access.
func (p *PRProcessor) createFallbackPR(repo *git.Repository, targetBranch string, commitMessages []string, conflictDetails string) error {
	pr := p.event.PullRequest

	// Create a new branch for the fallback PR
	branchName := fmt.Sprintf("pronto/%s/pr-%d", targetBranch, pr.GetNumber())

	log.Printf("Creating fallback branch: %s", branchName)
	if err := repo.CreateBranch(branchName); err != nil {
		return fmt.Errorf("failed to create fallback branch: %w", err)
	}

	// Push the branch
	log.Printf("Pushing fallback branch to origin")
	if err := repo.Push("origin", branchName, false); err != nil {
		return fmt.Errorf("failed to push fallback branch: %w", err)
	}

	// Create PR
	prBody := ghclient.FormatConflictPRBody(pr.GetNumber(), targetBranch, commitMessages, conflictDetails)
	prTitle := fmt.Sprintf("[PROnto] Cherry-pick PR #%d to %s", pr.GetNumber(), targetBranch)

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
		return fmt.Errorf("failed to create fallback PR: %w", err)
	}

	log.Printf("Created fallback PR #%d", newPR.GetNumber())

	// Add comment to original PR
	comment := fmt.Sprintf("✅ Created fallback PR #%d for cherry-picking to `%s`", newPR.GetNumber(), targetBranch)
	if err := p.ghClient.AddComment(p.ctx, pr.GetNumber(), comment); err != nil {
		log.Printf("Failed to add fallback PR comment: %v", err)
	}

	return nil
}

// createConflictPR creates a PR with conflict markers for manual resolution.
func (p *PRProcessor) createConflictPR(targetBranch string, commitMessages []string, conflictDetails string) error {
	pr := p.event.PullRequest

	// For conflicts, we need to create a branch with the conflicts preserved
	// Since we aborted the cherry-pick, we need to do it again but keep the conflicts
	// For now, just add a comment explaining the conflict

	// Extract commit SHAs for the instructions
	commitSHAs := extractCommitSHAsFromMessages(commitMessages)
	cherryPickCmd := "git cherry-pick " + strings.Join(commitSHAs, " ")

	comment := fmt.Sprintf(
		"⚠️ **PROnto Conflict**: Cherry-picking PR #%d to `%s` resulted in conflicts.\n\n"+
			"**Commits:**\n%s\n\n"+
			"**Conflict Details:**\n```\n%s\n```\n\n"+
			"### 🛠️ How to Resolve Conflicts\n\n"+
			"To resolve the conflicts locally, run:\n\n"+
			"```bash\n"+
			"# Checkout the target branch\n"+
			"git checkout %s\n"+
			"git pull origin %s\n\n"+
			"# Cherry-pick the commits\n"+
			"%s\n\n"+
			"# Resolve conflicts in the affected files\n"+
			"# Edit the conflicted files, then:\n"+
			"git add .\n"+
			"git cherry-pick --continue\n\n"+
			"# Push the changes\n"+
			"git push origin %s\n"+
			"```\n\n"+
			"---\n"+
			"🤖 Automated by [PROnto](https://github.com/theakshaypant/pronto)",
		pr.GetNumber(),
		targetBranch,
		formatCommitList(commitMessages),
		conflictDetails,
		targetBranch,
		targetBranch,
		cherryPickCmd,
		targetBranch,
	)

	if err := p.ghClient.AddComment(p.ctx, pr.GetNumber(), comment); err != nil {
		return fmt.Errorf("failed to add conflict comment: %w", err)
	}

	log.Printf("Added conflict comment to PR #%d for branch %s", pr.GetNumber(), targetBranch)
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

// formatCommitList formats commit messages as a bulleted list.
func formatCommitList(messages []string) string {
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString("- ")
		sb.WriteString(msg)
		sb.WriteString("\n")
	}
	return sb.String()
}

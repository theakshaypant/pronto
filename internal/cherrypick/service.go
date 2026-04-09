package cherrypick

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/theakshaypant/pronto/internal/git"
	ghclient "github.com/theakshaypant/pronto/internal/github"
	"github.com/theakshaypant/pronto/pkg/models"
)

// ProgressFunc is called with status updates during cherry-pick operations.
// Callers can use this to display progress in CLI or log in CI.
type ProgressFunc func(step string)

// Service performs cherry-pick operations independent of GitHub webhook events.
type Service struct {
	ctx      context.Context
	ghClient *ghclient.Client
	progress ProgressFunc
}

// NewService creates a new cherry-pick service.
func NewService(ctx context.Context, ghClient *ghclient.Client) *Service {
	return &Service{
		ctx:      ctx,
		ghClient: ghClient,
	}
}

// SetProgressFunc sets a callback for progress updates.
func (s *Service) SetProgressFunc(fn ProgressFunc) {
	s.progress = fn
}

func (s *Service) report(step string) {
	if s.progress != nil {
		s.progress(step)
	}
}

// CherryPickToTarget performs a cherry-pick of the given commits to a single target branch.
func (s *Service) CherryPickToTarget(opts CherryPickOptions) Result {
	target := opts.TargetBranch
	targetBranch := target.Name

	// Check if target branch exists
	s.report(fmt.Sprintf("Checking if branch %s exists...", targetBranch))
	exists, err := s.ghClient.BranchExists(s.ctx, targetBranch)
	if err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to check if branch exists: %v", err),
		}
	}

	// Handle branch creation if specified with .. notation
	if !exists && target.ShouldCreate {
		log.Printf("Branch %s does not exist, creating from %s", targetBranch, target.BaseBranch)
		s.report(fmt.Sprintf("Creating branch %s from %s...", targetBranch, target.BaseBranch))

		if result, ok := s.createTargetBranch(opts, target); !ok {
			return result
		}
	} else if !exists {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("⚠️ Branch does not exist. Use label `%s%s..base-branch` to create it automatically.", opts.LabelPattern, targetBranch),
		}
	}

	if opts.DryRun {
		msg := fmt.Sprintf("🔍 [dry-run] Would cherry-pick %d commit(s) to %s", len(opts.CommitSHAs), targetBranch)
		if opts.AlwaysCreatePR || !opts.HasWriteAccess {
			msg += " (via PR)"
		} else {
			msg += " (direct push)"
		}
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      true,
			Status:       "success",
			Message:      msg,
		}
	}

	// Create temporary directory for git operations
	tempDir, err := os.MkdirTemp("", "pronto-*")
	if err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to create temp directory: %v", err),
		}
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "repo")

	// Clone repository
	s.report("Cloning repository...")
	log.Printf("Cloning repository to %s", repoDir)
	repo, err := git.Clone(git.CloneOptions{
		URL:       opts.CloneURL,
		Token:     opts.Token,
		Directory: repoDir,
		Depth:     0, // Full clone needed for cherry-pick
	})
	if err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to clone repository: %v", err),
		}
	}

	// Configure git user
	if err := repo.ConfigUser(opts.BotName, opts.BotEmail); err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to configure git user: %v", err),
		}
	}

	// Checkout target branch
	s.report(fmt.Sprintf("Checking out %s...", targetBranch))
	log.Printf("Checking out target branch: %s", targetBranch)
	if err := repo.Checkout(targetBranch); err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to checkout target branch: %v", err),
		}
	}

	// Perform cherry-pick
	s.report(fmt.Sprintf("Cherry-picking %d commit(s)...", len(opts.CommitSHAs)))
	log.Printf("Cherry-picking %d commit(s)", len(opts.CommitSHAs))
	cpResult, err := repo.CherryPick(opts.CommitSHAs...)
	if err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Cherry-pick operation failed: %v", err),
		}
	}

	if cpResult.Success {
		return s.handleSuccess(repo, opts, target)
	}

	return s.handleConflict(repo, opts, targetBranch, cpResult)
}

// CherryPickBatch processes multiple PRs to multiple branches.
// The caller provides PR data via prInputs; the service handles the rest.
func (s *Service) CherryPickBatch(opts BatchOptions, prInputs []PRInput) []Result {
	var results []Result

	for _, pr := range prInputs {
		for _, target := range opts.TargetBranches {
			cpOpts := CherryPickOptions{
				Owner:          opts.Owner,
				Repo:           opts.Repo,
				CloneURL:       opts.CloneURL,
				Token:          opts.Token,
				PRNumber:       pr.PRNumber,
				TargetBranch:   target,
				CommitSHAs:     pr.CommitSHAs,
				CommitMessages: pr.CommitMessages,
				HasWriteAccess: opts.HasWriteAccess,
				AlwaysCreatePR: opts.AlwaysCreatePR,
				DryRun:         opts.DryRun,
				BotName:        opts.BotName,
				BotEmail:       opts.BotEmail,
				ConflictLabel:  opts.ConflictLabel,
				LabelPattern:   opts.LabelPattern,
			}

			result := s.CherryPickToTarget(cpOpts)
			results = append(results, result)
		}
	}

	return results
}

// createTargetBranch creates a target branch from its base branch.
// Returns (result, false) if creation fails; (_, true) if it succeeds.
func (s *Service) createTargetBranch(opts CherryPickOptions, target *models.TargetBranch) (Result, bool) {
	targetBranch := target.Name

	// Verify base branch exists
	baseExists, err := s.ghClient.BranchExists(s.ctx, target.BaseBranch)
	if err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to check if base branch `%s` exists: %v", target.BaseBranch, err),
		}, false
	}

	if !baseExists {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("⚠️ Cannot create branch because the base branch `%s` does not exist. Please check the label format or create the base branch first.", target.BaseBranch),
		}, false
	}

	// Get base branch SHA
	baseSHA, err := s.ghClient.GetBranchSHA(s.ctx, target.BaseBranch)
	if err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to get base branch SHA: %v", err),
		}, false
	}

	// Create the target branch
	if err := s.ghClient.CreateBranch(s.ctx, targetBranch, baseSHA); err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to create target branch: %v", err),
		}, false
	}

	log.Printf("Successfully created branch %s from %s", targetBranch, target.BaseBranch)
	return Result{}, true
}

// handleSuccess handles a successful cherry-pick — push directly or create a PR.
func (s *Service) handleSuccess(repo *git.Repository, opts CherryPickOptions, target *models.TargetBranch) Result {
	targetBranch := target.Name

	if opts.HasWriteAccess && !opts.AlwaysCreatePR {
		// Push directly
		s.report(fmt.Sprintf("Pushing to %s...", targetBranch))
		log.Printf("Pushing cherry-picked commits to %s", targetBranch)
		if err := repo.Push("origin", targetBranch, false); err != nil {
			return Result{
				PRNumber:     opts.PRNumber,
				TargetBranch: targetBranch,
				Success:      false,
				Status:       "failed",
				Message:      fmt.Sprintf("❌ Failed to push to target branch: %s", git.SanitizeError(err)),
			}
		}

		log.Printf("Successfully cherry-picked to %s", targetBranch)

		// Create tag if specified
		if target.TagName != "" {
			if result, ok := s.createAndPushTag(repo, opts, target); !ok {
				return result
			}
		}

		// Build success message
		msg := fmt.Sprintf("✅ Successfully cherry-picked %d commit(s)", len(opts.CommitMessages))
		if target.ShouldCreate {
			msg = fmt.Sprintf("✅ Created branch from `%s` and cherry-picked %d commit(s)", target.BaseBranch, len(opts.CommitMessages))
		}
		if target.TagName != "" {
			msg += fmt.Sprintf(", created tag `%s`", target.TagName)
		}

		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      true,
			Status:       "success",
			Message:      msg,
		}
	}

	// Create PR instead of pushing directly
	reason := "user lacks write access"
	if opts.AlwaysCreatePR {
		reason = "always_create_pr is enabled"
	}
	log.Printf("Creating PR (%s)", reason)
	return s.createCherryPickPR(repo, opts, target)
}

// createCherryPickPR creates a PR for cherry-picked changes.
func (s *Service) createCherryPickPR(repo *git.Repository, opts CherryPickOptions, target *models.TargetBranch) Result {
	targetBranch := target.Name
	branchName := fmt.Sprintf("pronto/%s/pr-%d", targetBranch, opts.PRNumber)

	s.report("Creating cherry-pick PR...")
	log.Printf("Creating cherry-pick branch: %s", branchName)
	if err := repo.CreateBranch(branchName); err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to create cherry-pick branch: %v", err),
		}
	}

	log.Printf("Pushing cherry-pick branch to origin")
	if err := repo.Push("origin", branchName, false); err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to push cherry-pick branch: %s", git.SanitizeError(err)),
		}
	}

	prBody := ghclient.FormatConflictPRBody(opts.PRNumber, targetBranch, opts.CommitMessages, "", target.TagName)
	prTitle := fmt.Sprintf("[PROnto] Cherry-pick PR #%d to %s", opts.PRNumber, targetBranch)

	labels := []string{"pronto"}

	newPR, err := s.ghClient.CreatePullRequest(s.ctx, ghclient.PROptions{
		Title:  prTitle,
		Body:   prBody,
		Head:   branchName,
		Base:   targetBranch,
		Labels: labels,
	})
	if err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to create cherry-pick PR: %v", err),
		}
	}

	log.Printf("Created cherry-pick PR #%d", newPR.GetNumber())

	msg := fmt.Sprintf("✅ Created PR #%d", newPR.GetNumber())
	if target.ShouldCreate {
		msg = fmt.Sprintf("✅ Created branch from `%s` and created PR #%d", target.BaseBranch, newPR.GetNumber())
	}
	if target.TagName != "" {
		msg += fmt.Sprintf(" (tag `%s` pending merge)", target.TagName)
	}

	return Result{
		PRNumber:     opts.PRNumber,
		TargetBranch: targetBranch,
		Success:      true,
		Status:       "pending_pr",
		Message:      msg,
		CreatedPR:    newPR.GetNumber(),
	}
}

// handleConflict handles a cherry-pick with conflicts — creates a conflict branch and PR.
func (s *Service) handleConflict(repo *git.Repository, opts CherryPickOptions, targetBranch string, cpResult *git.CherryPickResult) Result {
	log.Printf("Cherry-pick resulted in conflicts for branch %s", targetBranch)

	// Get conflict details while we still have the conflicts in working tree
	details, err := repo.GetConflictDetails()
	if err != nil {
		log.Printf("Failed to get conflict details: %v", err)
		details = fmt.Sprintf("Conflicts in %d file(s)", len(cpResult.ConflictedFiles))
	}

	// Stage all files (including conflicts with markers)
	if err := repo.StageAll(); err != nil {
		repo.AbortCherryPick()
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to stage conflicted files: %v", err),
		}
	}

	// Commit the conflicts to complete the cherry-pick
	commitMsg := fmt.Sprintf("Cherry-pick PR #%d to %s (conflicts)\n\nConflicts need manual resolution", opts.PRNumber, targetBranch)
	if err := repo.Commit(commitMsg); err != nil {
		repo.AbortCherryPick()
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to commit conflicts: %v", err),
		}
	}

	// Create a new branch from this commit
	conflictBranch := fmt.Sprintf("pronto/%s/pr-%d-conflicts", targetBranch, opts.PRNumber)
	log.Printf("Creating conflict branch: %s", conflictBranch)

	if err := repo.CreateBranch(conflictBranch); err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to create conflict branch: %v", err),
		}
	}

	// Push the conflict branch (force push in case it exists from a previous attempt)
	s.report("Pushing conflict branch...")
	log.Printf("Pushing conflict branch to origin (force)")
	if err := repo.Push("origin", conflictBranch, true); err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to push conflict branch: %s", git.SanitizeError(err)),
		}
	}

	// Checkout back to the base branch and reset to remove the conflict commit
	if err := repo.Checkout(targetBranch); err != nil {
		log.Printf("Warning: Failed to checkout back to %s: %v", targetBranch, err)
	} else {
		if err := repo.ResetHard("HEAD~1"); err != nil {
			log.Printf("Warning: Failed to reset %s: %v", targetBranch, err)
		}
	}

	// Create PR with conflict label
	prBody := ghclient.FormatConflictPRBody(opts.PRNumber, targetBranch, opts.CommitMessages, details, opts.TargetBranch.TagName)
	prTitle := fmt.Sprintf("[PROnto] Cherry-pick PR #%d to %s (CONFLICTS)", opts.PRNumber, targetBranch)

	labels := []string{"pronto", opts.ConflictLabel}

	newPR, err := s.ghClient.CreatePullRequest(s.ctx, ghclient.PROptions{
		Title:  prTitle,
		Body:   prBody,
		Head:   conflictBranch,
		Base:   targetBranch,
		Labels: labels,
	})
	if err != nil {
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Failed to create conflict PR: %v", err),
		}
	}

	log.Printf("Created conflict PR #%d", newPR.GetNumber())

	return Result{
		PRNumber:     opts.PRNumber,
		TargetBranch: targetBranch,
		Success:      false,
		Status:       "conflict",
		Message:      fmt.Sprintf("⚠️ Conflicts detected - created PR #%d for manual resolution", newPR.GetNumber()),
		CreatedPR:    newPR.GetNumber(),
	}
}

// createAndPushTag creates a tag and pushes it.
// Returns (result, false) on failure; (_, true) on success.
func (s *Service) createAndPushTag(repo *git.Repository, opts CherryPickOptions, target *models.TargetBranch) (Result, bool) {
	targetBranch := target.Name
	log.Printf("Creating tag %s for branch %s", target.TagName, targetBranch)

	tagMessage := fmt.Sprintf("Cherry-picked PR #%d to %s\n\nOriginal PR: #%d\nCommits: %d",
		opts.PRNumber, targetBranch, opts.PRNumber, len(opts.CommitMessages))

	if err := repo.CreateTag(target.TagName, tagMessage); err != nil {
		log.Printf("Failed to create tag %s: %v", target.TagName, err)
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Cherry-pick succeeded but tag creation failed: %v", err),
		}, false
	}

	if err := repo.PushTag("origin", target.TagName); err != nil {
		log.Printf("Failed to push tag %s: %v", target.TagName, git.SanitizeError(err))
		return Result{
			PRNumber:     opts.PRNumber,
			TargetBranch: targetBranch,
			Success:      false,
			Status:       "failed",
			Message:      fmt.Sprintf("❌ Cherry-pick and tag creation succeeded but tag push failed: %s", git.SanitizeError(err)),
		}, false
	}

	log.Printf("Successfully created and pushed tag %s", target.TagName)
	return Result{}, true
}

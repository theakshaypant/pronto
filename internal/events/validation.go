package events

import (
	"fmt"

	"github.com/google/go-github/v81/github"
)

// validatePREvent performs comprehensive validation on a pull request event.
func validatePREvent(event *github.PullRequestEvent) error {
	if event == nil {
		return fmt.Errorf("pull request event is nil")
	}

	if event.PullRequest == nil {
		return fmt.Errorf("pull request is nil in event")
	}

	if event.Repo == nil {
		return fmt.Errorf("repository is nil in event")
	}

	pr := event.PullRequest

	// Validate PR fields
	if pr.Number == nil {
		return fmt.Errorf("pull request number is nil")
	}

	if pr.Head == nil {
		return fmt.Errorf("pull request head is nil")
	}

	if pr.Head.SHA == nil {
		return fmt.Errorf("pull request head SHA is nil")
	}

	if pr.User == nil {
		return fmt.Errorf("pull request user is nil")
	}

	if pr.User.Login == nil {
		return fmt.Errorf("pull request user login is nil")
	}

	// Validate repository fields
	repo := event.Repo

	if repo.Owner == nil {
		return fmt.Errorf("repository owner is nil")
	}

	if repo.Owner.Login == nil {
		return fmt.Errorf("repository owner login is nil")
	}

	if repo.Name == nil {
		return fmt.Errorf("repository name is nil")
	}

	if repo.CloneURL == nil {
		return fmt.Errorf("repository clone URL is nil")
	}

	return nil
}

// validateCommits ensures commits are valid for cherry-picking.
func validateCommits(commits []*github.RepositoryCommit) error {
	if len(commits) == 0 {
		return fmt.Errorf("no commits found in pull request")
	}

	for i, commit := range commits {
		if commit == nil {
			return fmt.Errorf("commit at index %d is nil", i)
		}

		if commit.SHA == nil {
			return fmt.Errorf("commit at index %d has nil SHA", i)
		}

		if commit.Commit == nil {
			return fmt.Errorf("commit at index %d has nil commit data", i)
		}
	}

	return nil
}

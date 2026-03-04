package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v81/github"
)

// PROptions contains options for creating a pull request.
type PROptions struct {
	Title  string
	Body   string
	Head   string
	Base   string
	Labels []string
}

// CreatePullRequest creates a new pull request.
func (c *Client) CreatePullRequest(ctx context.Context, opts PROptions) (*github.PullRequest, error) {
	newPR := &github.NewPullRequest{
		Title: github.Ptr(opts.Title),
		Body:  github.Ptr(opts.Body),
		Head:  github.Ptr(opts.Head),
		Base:  github.Ptr(opts.Base),
	}

	pr, _, err := c.client.PullRequests.Create(ctx, c.owner, c.repo, newPR)
	if err != nil {
		return nil, fmt.Errorf("failed to create pull request: %w", err)
	}

	// Add labels if specified
	if len(opts.Labels) > 0 {
		if err := c.AddLabels(ctx, pr.GetNumber(), opts.Labels); err != nil {
			// Log error but don't fail - PR was created successfully
			return pr, fmt.Errorf("pull request created but failed to add labels: %w", err)
		}
	}

	return pr, nil
}

// GetPullRequest retrieves a pull request by number.
func (c *Client) GetPullRequest(ctx context.Context, number int) (*github.PullRequest, error) {
	pr, _, err := c.client.PullRequests.Get(ctx, c.owner, c.repo, number)
	if err != nil {
		return nil, fmt.Errorf("failed to get pull request: %w", err)
	}

	return pr, nil
}

// GetPullRequestCommits retrieves all commits for a pull request.
func (c *Client) GetPullRequestCommits(ctx context.Context, number int) ([]*github.RepositoryCommit, error) {
	var allCommits []*github.RepositoryCommit
	opts := &github.ListOptions{PerPage: 100}

	for {
		commits, resp, err := c.client.PullRequests.ListCommits(ctx, c.owner, c.repo, number, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to get pull request commits: %w", err)
		}

		allCommits = append(allCommits, commits...)

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allCommits, nil
}

// AddComment adds a comment to a pull request or issue.
func (c *Client) AddComment(ctx context.Context, number int, body string) error {
	comment := &github.IssueComment{
		Body: github.Ptr(body),
	}

	_, _, err := c.client.Issues.CreateComment(ctx, c.owner, c.repo, number, comment)
	if err != nil {
		return fmt.Errorf("failed to add comment: %w", err)
	}

	return nil
}

// AddLabels adds labels to a pull request or issue.
func (c *Client) AddLabels(ctx context.Context, number int, labels []string) error {
	_, _, err := c.client.Issues.AddLabelsToIssue(ctx, c.owner, c.repo, number, labels)
	if err != nil {
		return fmt.Errorf("failed to add labels: %w", err)
	}

	return nil
}

// RemoveLabel removes a label from a pull request or issue.
func (c *Client) RemoveLabel(ctx context.Context, number int, label string) error {
	_, err := c.client.Issues.RemoveLabelForIssue(ctx, c.owner, c.repo, number, label)
	if err != nil {
		return fmt.Errorf("failed to remove label: %w", err)
	}

	return nil
}

// FormatConflictPRBody generates the body text for a conflict PR.
func FormatConflictPRBody(sourcePR int, targetBranch string, commits []string, conflictDetails string, tagName string) string {
	var sb strings.Builder

	if conflictDetails != "" {
		// Conflict PR
		sb.WriteString("## 🔀 Cherry-pick Conflict\n\n")
		sb.WriteString(fmt.Sprintf("This PR was automatically created because cherry-picking PR #%d to `%s` resulted in conflicts.\n\n", sourcePR, targetBranch))
	} else {
		// Cherry-pick PR
		sb.WriteString("## 🤖 Automated Cherry-pick\n\n")
		sb.WriteString(fmt.Sprintf("This PR contains the cherry-picked commits from PR #%d to `%s`.\n\n", sourcePR, targetBranch))
	}

	sb.WriteString("### Original PR\n\n")
	sb.WriteString(fmt.Sprintf("- Source: #%d\n", sourcePR))
	sb.WriteString(fmt.Sprintf("- Target branch: `%s`\n\n", targetBranch))

	sb.WriteString("### Commits\n\n")
	for _, commit := range commits {
		sb.WriteString(fmt.Sprintf("- %s\n", commit))
	}

	if conflictDetails != "" {
		sb.WriteString("\n### ⚠️ Conflict Details\n\n")
		sb.WriteString("```\n")
		sb.WriteString(conflictDetails)
		sb.WriteString("\n```\n")

		sb.WriteString("\n### 🛠️ How to Resolve Conflicts\n\n")
		sb.WriteString("To resolve the conflicts locally, run the following commands:\n\n")
		sb.WriteString("```bash\n")
		sb.WriteString("# Checkout the target branch\n")
		sb.WriteString(fmt.Sprintf("git checkout %s\n", targetBranch))
		sb.WriteString("git pull origin " + targetBranch + "\n\n")

		sb.WriteString("# Cherry-pick the commits\n")

		// Extract commit SHAs from commit messages
		commitSHAs := extractCommitSHAs(commits)
		if len(commitSHAs) > 0 {
			sb.WriteString("git cherry-pick")
			for _, sha := range commitSHAs {
				sb.WriteString(" " + sha)
			}
			sb.WriteString("\n\n")
		}

		sb.WriteString("# Resolve conflicts in the affected files\n")
		sb.WriteString("# Edit the conflicted files, then:\n")
		sb.WriteString("git add .\n")
		sb.WriteString("git cherry-pick --continue\n\n")

		sb.WriteString("# Push to this PR's branch\n")
		sb.WriteString(fmt.Sprintf("git push origin %s\n", targetBranch))
		sb.WriteString("```\n\n")

		sb.WriteString("Alternatively, you can resolve conflicts directly in this PR by editing the files through GitHub's web interface.\n\n")
	} else {
		sb.WriteString("\n### Next Steps\n\n")
		sb.WriteString("Review the changes and merge this PR if everything looks correct.\n\n")
	}

	// Add tag creation instructions if a tag is specified
	if tagName != "" {
		sb.WriteString("### 🏷️ Tag Creation Required\n\n")
		sb.WriteString(fmt.Sprintf("After merging this PR, create the tag `%s` by running:\n\n", tagName))
		sb.WriteString("```bash\n")
		sb.WriteString(fmt.Sprintf("git checkout %s\n", targetBranch))
		sb.WriteString(fmt.Sprintf("git pull origin %s\n", targetBranch))
		sb.WriteString(fmt.Sprintf("git tag -a %s -m \"Cherry-picked PR #%d to %s\"\n", tagName, sourcePR, targetBranch))
		sb.WriteString(fmt.Sprintf("git push origin %s\n", tagName))
		sb.WriteString("```\n\n")
	}

	sb.WriteString("---\n")
	sb.WriteString("🤖 Automated by [PROnto](https://github.com/theakshaypant/pronto)\n")

	return sb.String()
}

// extractCommitSHAs extracts commit SHAs from commit messages.
// Commit messages are formatted as "Message (abc1234)"
func extractCommitSHAs(commits []string) []string {
	var shas []string
	for _, commit := range commits {
		// Find SHA in parentheses at the end: "Message (abc1234)"
		if idx := strings.LastIndex(commit, "("); idx != -1 {
			if endIdx := strings.LastIndex(commit, ")"); endIdx > idx {
				sha := commit[idx+1 : endIdx]
				shas = append(shas, sha)
			}
		}
	}
	return shas
}

// FormatMissingBranchComment generates a comment for when a target branch doesn't exist.
func FormatMissingBranchComment(branchName, labelPattern string) string {
	return fmt.Sprintf(
		"⚠️ **PROnto Notice**: Cannot cherry-pick to `%s` because the branch does not exist.\n\n"+
			"Please create the branch first or remove the `%s%s` label.\n\n"+
			"---\n"+
			"🤖 Automated by [PROnto](https://github.com/theakshaypant/pronto)",
		branchName,
		labelPattern,
		branchName,
	)
}

// FormatNoPermissionComment generates a comment for when a user lacks write permissions.
func FormatNoPermissionComment(username, targetBranch string) string {
	return fmt.Sprintf(
		"⚠️ **PROnto Notice**: User @%s does not have write permissions to this repository.\n\n"+
			"A cherry-pick pull request will be created for cherry-picking to `%s` instead of automatically pushing changes.\n\n"+
			"---\n"+
			"🤖 Automated by [PROnto](https://github.com/theakshaypant/pronto)",
		username,
		targetBranch,
	)
}

// FormatSuccessComment generates a comment for successful cherry-pick.
func FormatSuccessComment(targetBranch string, commits []string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("✅ **PROnto Success**: Cherry-picked to `%s`\n\n", targetBranch))

	sb.WriteString("### Commits\n\n")
	for _, commit := range commits {
		sb.WriteString(fmt.Sprintf("- %s\n", commit))
	}

	sb.WriteString("\n---\n")
	sb.WriteString("🤖 Automated by [PROnto](https://github.com/theakshaypant/pronto)")

	return sb.String()
}

package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v81/github"
)

// GetIssue retrieves an issue by number.
func (c *Client) GetIssue(ctx context.Context, number int) (*github.Issue, error) {
	issue, _, err := c.client.Issues.Get(ctx, c.owner, c.repo, number)
	if err != nil {
		return nil, fmt.Errorf("failed to get issue: %w", err)
	}

	return issue, nil
}

// SearchIssues searches for issues matching a query string.
// Query uses GitHub search syntax (e.g., "is:open is:issue in:title [pronto]")
func (c *Client) SearchIssues(ctx context.Context, query string) ([]*github.Issue, error) {
	// Add repository qualifier to the query
	fullQuery := fmt.Sprintf("%s repo:%s/%s", query, c.owner, c.repo)

	opts := &github.SearchOptions{
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	var allIssues []*github.Issue

	for {
		result, resp, err := c.client.Search.Issues(ctx, fullQuery, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to search issues: %w", err)
		}

		allIssues = append(allIssues, result.Issues...)

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allIssues, nil
}

// ListIssueComments retrieves all comments on an issue.
func (c *Client) ListIssueComments(ctx context.Context, issueNumber int) ([]*github.IssueComment, error) {
	var allComments []*github.IssueComment
	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		comments, resp, err := c.client.Issues.ListComments(ctx, c.owner, c.repo, issueNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list issue comments: %w", err)
		}

		allComments = append(allComments, comments...)

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allComments, nil
}

// UpdateComment updates an existing comment on an issue or PR.
func (c *Client) UpdateComment(ctx context.Context, commentID int64, body string) error {
	comment := &github.IssueComment{
		Body: github.Ptr(body),
	}

	_, _, err := c.client.Issues.EditComment(ctx, c.owner, c.repo, commentID, comment)
	if err != nil {
		return fmt.Errorf("failed to update comment: %w", err)
	}

	return nil
}

// FindProntoComment searches for an existing PROnto comment in an issue.
// Returns the comment if found, or nil if not found.
func (c *Client) FindProntoComment(ctx context.Context, issueNumber int, marker string) (*github.IssueComment, error) {
	comments, err := c.ListIssueComments(ctx, issueNumber)
	if err != nil {
		return nil, err
	}

	// Search for comment containing the marker
	for _, comment := range comments {
		if comment.Body != nil && contains(*comment.Body, marker) {
			return comment, nil
		}
	}

	return nil, nil
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

// findSubstring performs a simple substring search.
func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

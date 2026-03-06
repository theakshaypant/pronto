package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v81/github"
)

// BranchExists checks if a branch exists in the repository.
func (c *Client) BranchExists(ctx context.Context, branchName string) (bool, error) {
	_, resp, err := c.client.Repositories.GetBranch(ctx, c.owner, c.repo, branchName, 0)

	if err != nil {
		// 404 means branch doesn't exist
		if resp != nil && resp.StatusCode == 404 {
			return false, nil
		}
		return false, fmt.Errorf("failed to check branch existence: %w", err)
	}

	return true, nil
}

// GetBranchSHA retrieves the SHA of a branch's HEAD commit.
func (c *Client) GetBranchSHA(ctx context.Context, branchName string) (string, error) {
	branch, _, err := c.client.Repositories.GetBranch(ctx, c.owner, c.repo, branchName, 0)
	if err != nil {
		return "", fmt.Errorf("failed to get branch: %w", err)
	}

	if branch.Commit == nil || branch.Commit.SHA == nil {
		return "", fmt.Errorf("branch commit SHA is nil")
	}

	return *branch.Commit.SHA, nil
}

// CreateBranch creates a new branch from a base SHA.
func (c *Client) CreateBranch(ctx context.Context, branchName, baseSHA string) error {
	ref := fmt.Sprintf("refs/heads/%s", branchName)

	createRef := github.CreateRef{
		Ref: ref,
		SHA: baseSHA,
	}

	_, _, err := c.client.Git.CreateRef(ctx, c.owner, c.repo, createRef)
	if err != nil {
		return fmt.Errorf("failed to create branch: %w", err)
	}

	return nil
}

// DeleteBranch deletes a branch from the repository.
func (c *Client) DeleteBranch(ctx context.Context, branchName string) error {
	ref := fmt.Sprintf("refs/heads/%s", branchName)

	_, err := c.client.Git.DeleteRef(ctx, c.owner, c.repo, ref)
	if err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}

	return nil
}

// CreateTag creates an annotated tag at the specified commit SHA.
func (c *Client) CreateTag(ctx context.Context, tagName, sha, message string) error {
	// First, create the tag object (annotated tag)
	tagType := "commit"
	tag := github.CreateTag{
		Tag:     tagName,
		Message: message,
		Object:  sha,
		Type:    tagType,
	}

	createdTag, _, err := c.client.Git.CreateTag(ctx, c.owner, c.repo, tag)
	if err != nil {
		return fmt.Errorf("failed to create tag object: %w", err)
	}

	// Then, create the reference to the tag
	ref := fmt.Sprintf("refs/tags/%s", tagName)
	createRef := github.CreateRef{
		Ref: ref,
		SHA: *createdTag.SHA,
	}

	_, _, err = c.client.Git.CreateRef(ctx, c.owner, c.repo, createRef)
	if err != nil {
		return fmt.Errorf("failed to create tag reference: %w", err)
	}

	return nil
}

package permissions

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v81/github"
)

// Level represents a GitHub permission level.
type Level string

const (
	LevelNone  Level = "none"
	LevelRead  Level = "read"
	LevelWrite Level = "write"
	LevelAdmin Level = "admin"
)

// Checker validates user permissions for repository operations.
type Checker struct {
	client *github.Client
	owner  string
	repo   string
	cache  *cache
}

// NewChecker creates a new permission checker.
func NewChecker(client *github.Client, owner, repo string) *Checker {
	return &Checker{
		client: client,
		owner:  owner,
		repo:   repo,
		cache:  newCache(),
	}
}

// HasWriteAccess checks if a user has write, maintain, or admin permissions.
func (c *Checker) HasWriteAccess(ctx context.Context, username string) (bool, error) {
	if username == "" {
		return false, fmt.Errorf("username cannot be empty")
	}

	// Check cache first
	if level, found := c.cache.get(username); found {
		return isWriteLevel(level), nil
	}

	// Query GitHub API for user's permission level
	perm, _, err := c.client.Repositories.GetPermissionLevel(ctx, c.owner, c.repo, username)
	if err != nil {
		return false, fmt.Errorf("failed to get permission level for user %s: %w", username, err)
	}

	if perm.Permission == nil {
		return false, fmt.Errorf("permission level is nil for user %s", username)
	}

	level := Level(strings.ToLower(*perm.Permission))

	// Cache the result
	c.cache.set(username, level)

	return isWriteLevel(level), nil
}

// GetPermissionLevel retrieves the permission level for a user.
func (c *Checker) GetPermissionLevel(ctx context.Context, username string) (Level, error) {
	if username == "" {
		return LevelNone, fmt.Errorf("username cannot be empty")
	}

	// Check cache first
	if level, found := c.cache.get(username); found {
		return level, nil
	}

	// Query GitHub API
	perm, _, err := c.client.Repositories.GetPermissionLevel(ctx, c.owner, c.repo, username)
	if err != nil {
		return LevelNone, fmt.Errorf("failed to get permission level for user %s: %w", username, err)
	}

	if perm.Permission == nil {
		return LevelNone, fmt.Errorf("permission level is nil for user %s", username)
	}

	level := Level(strings.ToLower(*perm.Permission))

	// Cache the result
	c.cache.set(username, level)

	return level, nil
}

// isWriteLevel returns true if the permission level allows write access.
func isWriteLevel(level Level) bool {
	switch level {
	case LevelWrite, LevelAdmin:
		return true
	default:
		return false
	}
}

// ClearCache clears the permission cache.
func (c *Checker) ClearCache() {
	c.cache.clear()
}

package models

import (
	"errors"
	"fmt"
)

// Common error variables for comparison
var (
	// ErrBranchNotFound indicates the target branch does not exist
	ErrBranchNotFound = errors.New("branch not found")

	// ErrPermissionDenied indicates the user lacks required permissions
	ErrPermissionDenied = errors.New("permission denied")

	// ErrConflict indicates a cherry-pick resulted in conflicts
	ErrConflict = errors.New("cherry-pick conflict")

	// ErrInvalidInput indicates invalid input parameters
	ErrInvalidInput = errors.New("invalid input")

	// ErrNotMerged indicates the PR is not merged
	ErrNotMerged = errors.New("pull request not merged")
)

// BranchNotFoundError represents a missing target branch error.
type BranchNotFoundError struct {
	Branch string
}

func (e *BranchNotFoundError) Error() string {
	return fmt.Sprintf("target branch %q does not exist", e.Branch)
}

func (e *BranchNotFoundError) Is(target error) bool {
	return target == ErrBranchNotFound
}

// ConflictError represents a cherry-pick conflict error.
type ConflictError struct {
	Branch          string
	ConflictedFiles []string
	Details         string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("cherry-pick to %q resulted in %d conflict(s)", e.Branch, len(e.ConflictedFiles))
}

func (e *ConflictError) Is(target error) bool {
	return target == ErrConflict
}

// PermissionError represents a permission denied error.
type PermissionError struct {
	User   string
	Action string
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("user %q does not have permission to %s", e.User, e.Action)
}

func (e *PermissionError) Is(target error) bool {
	return target == ErrPermissionDenied
}

// ValidationError represents an input validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation error for %s: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation error: %s", e.Message)
}

func (e *ValidationError) Is(target error) bool {
	return target == ErrInvalidInput
}

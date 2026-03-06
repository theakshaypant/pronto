package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CherryPickResult represents the result of a cherry-pick operation.
type CherryPickResult struct {
	Success         bool
	ConflictedFiles []string
	CommitSHAs      []string
	ErrorOutput     string
}

// CherryPick attempts to cherry-pick commits onto the current branch.
// If conflicts occur, it leaves the repository in a conflicted state for the caller to handle.
// The caller MUST call AbortCherryPick() if they don't want to resolve the conflicts.
func (r *Repository) CherryPick(commitSHAs ...string) (*CherryPickResult, error) {
	if len(commitSHAs) == 0 {
		return nil, fmt.Errorf("no commits provided for cherry-pick")
	}

	result := &CherryPickResult{
		CommitSHAs: commitSHAs,
	}

	// Build cherry-pick command
	args := append([]string{"cherry-pick"}, commitSHAs...)

	cmd := exec.Command("git", args...)
	cmd.Dir = r.path

	output, err := cmd.CombinedOutput()

	if err != nil {
		// Check if cherry-pick resulted in empty commit (changes already applied)
		if isEmptyCommitError(output) {
			// Abort the empty cherry-pick
			r.AbortCherryPick()
			result.Success = true
			result.ErrorOutput = "Changes already applied (empty commit)"
			return result, nil
		}

		// Cherry-pick failed - check if it's due to conflicts
		if isConflictError(output) {
			result.Success = false
			result.ErrorOutput = string(output)

			// Get list of conflicted files
			conflicted, conflictErr := r.GetConflictedFiles()
			if conflictErr != nil {
				// Abort cherry-pick before returning error
				r.AbortCherryPick()
				return result, fmt.Errorf("cherry-pick failed with conflicts, and failed to get conflicted files: %w", conflictErr)
			}

			result.ConflictedFiles = conflicted

			// DON'T abort here - let the caller decide how to handle conflicts
			// The caller MUST call AbortCherryPick() if they don't resolve the conflicts

			return result, nil
		}

		// Non-conflict error
		return nil, fmt.Errorf("cherry-pick failed: %w\nOutput: %s", err, string(output))
	}

	// Success
	result.Success = true
	return result, nil
}

// AbortCherryPick aborts an in-progress cherry-pick.
func (r *Repository) AbortCherryPick() error {
	cmd := exec.Command("git", "cherry-pick", "--abort")
	cmd.Dir = r.path

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if there's no cherry-pick in progress
		if strings.Contains(string(output), "no cherry-pick") {
			return nil
		}
		return fmt.Errorf("failed to abort cherry-pick: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// GetConflictedFiles returns a list of files with merge conflicts.
func (r *Repository) GetConflictedFiles() ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = r.path

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get conflicted files: %w\nOutput: %s", err, string(output))
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")

	// Filter out empty strings
	var conflicted []string
	for _, file := range files {
		if file != "" {
			conflicted = append(conflicted, file)
		}
	}

	return conflicted, nil
}

// GetConflictDetails returns detailed conflict information for reporting.
func (r *Repository) GetConflictDetails() (string, error) {
	conflicted, err := r.GetConflictedFiles()
	if err != nil {
		return "", err
	}

	if len(conflicted) == 0 {
		return "No conflicts detected", nil
	}

	var details strings.Builder
	details.WriteString("Conflicted files:\n")

	for _, file := range conflicted {
		details.WriteString(fmt.Sprintf("  - %s\n", file))

		// Get conflict markers preview
		preview, err := r.getConflictPreview(file)
		if err == nil && preview != "" {
			details.WriteString(fmt.Sprintf("    %s\n", preview))
		}
	}

	return details.String(), nil
}

// getConflictPreview reads a file and returns a preview of conflict markers.
func (r *Repository) getConflictPreview(filename string) (string, error) {
	filePath := filepath.Join(r.path, filename)

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(content), "\n")
	conflictLines := 0

	for _, line := range lines {
		if strings.HasPrefix(line, "<<<<<<<") ||
			strings.HasPrefix(line, "=======") ||
			strings.HasPrefix(line, ">>>>>>>") {
			conflictLines++
		}
	}

	if conflictLines > 0 {
		return fmt.Sprintf("%d conflict marker(s) found", conflictLines/3), nil
	}

	return "", nil
}

// isConflictError checks if git output indicates a merge conflict.
func isConflictError(output []byte) bool {
	outputStr := strings.ToLower(string(output))

	conflictIndicators := []string{
		"conflict",
		"merge conflict",
		"conflicting",
		"could not apply",
	}

	for _, indicator := range conflictIndicators {
		if strings.Contains(outputStr, indicator) {
			return true
		}
	}

	return false
}

// isEmptyCommitError checks if git output indicates an empty commit (changes already applied).
func isEmptyCommitError(output []byte) bool {
	outputStr := string(output)

	emptyCommitIndicators := []string{
		"The previous cherry-pick is now empty",
		"nothing to commit, working tree clean",
		"no changes added to commit",
	}

	for _, indicator := range emptyCommitIndicators {
		if strings.Contains(outputStr, indicator) {
			return true
		}
	}

	return false
}

// HasConflicts checks if the repository has any unresolved conflicts.
func (r *Repository) HasConflicts() (bool, error) {
	conflicted, err := r.GetConflictedFiles()
	if err != nil {
		return false, err
	}

	return len(conflicted) > 0, nil
}

// StageAll stages all changes in the working directory.
func (r *Repository) StageAll() error {
	if err := r.exec("add", "-A"); err != nil {
		return fmt.Errorf("failed to stage changes: %w", err)
	}
	return nil
}

// Commit creates a new commit.
func (r *Repository) Commit(message string) error {
	if err := r.exec("commit", "-m", message); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}
	return nil
}

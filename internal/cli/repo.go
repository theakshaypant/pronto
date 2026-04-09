package cli

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// InferRepo auto-detects the owner/repo from the local git remote origin.
// Supports both HTTPS and SSH URL formats.
func InferRepo() (owner, repo string, err error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("failed to get git remote URL: %w (are you in a git repository?)", err)
	}

	url := strings.TrimSpace(string(output))
	return ParseRepoURL(url)
}

// ParseRepoURL extracts owner and repo from a git remote URL.
// Supported formats:
//   - https://github.com/owner/repo.git
//   - https://github.com/owner/repo
//   - git@github.com:owner/repo.git
//   - git@github.com:owner/repo
//   - ssh://git@github.com/owner/repo.git
func ParseRepoURL(url string) (owner, repo string, err error) {
	// SSH format: git@github.com:owner/repo.git
	sshPattern := regexp.MustCompile(`^git@[^:]+:([^/]+)/([^/]+?)(?:\.git)?$`)
	if matches := sshPattern.FindStringSubmatch(url); matches != nil {
		return matches[1], matches[2], nil
	}

	// HTTPS or SSH-over-HTTPS format: https://github.com/owner/repo.git
	// Also matches ssh://git@github.com/owner/repo.git
	httpsPattern := regexp.MustCompile(`^(?:https?|ssh)://[^/]+/([^/]+)/([^/]+?)(?:\.git)?$`)
	if matches := httpsPattern.FindStringSubmatch(url); matches != nil {
		return matches[1], matches[2], nil
	}

	return "", "", fmt.Errorf("could not parse owner/repo from remote URL: %s", url)
}

// InferCloneURL constructs the HTTPS clone URL for a given owner/repo.
func InferCloneURL(owner, repo string) string {
	return fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
}

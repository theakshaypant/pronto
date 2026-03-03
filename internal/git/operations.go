package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repository represents a local git repository.
type Repository struct {
	path   string
	remote string
	token  string
}

// CloneOptions contains options for cloning a repository.
type CloneOptions struct {
	URL       string
	Token     string
	Directory string
	Depth     int
}

// Clone creates a shallow clone of a repository.
func Clone(opts CloneOptions) (*Repository, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("repository URL cannot be empty")
	}

	if opts.Directory == "" {
		return nil, fmt.Errorf("clone directory cannot be empty")
	}

	// Create temporary directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(opts.Directory), 0755); err != nil {
		return nil, fmt.Errorf("failed to create clone directory: %w", err)
	}

	// Build authenticated URL
	authURL := opts.URL
	if opts.Token != "" {
		// Insert token into HTTPS URL: https://token@github.com/...
		authURL = strings.Replace(opts.URL, "https://", fmt.Sprintf("https://%s@", opts.Token), 1)
	}

	// Build clone command
	args := []string{"clone"}

	if opts.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", opts.Depth))
	}

	args = append(args, authURL, opts.Directory)

	// Execute clone
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git clone failed: %w\nOutput: %s", err, string(output))
	}

	return &Repository{
		path:   opts.Directory,
		remote: opts.URL,
		token:  opts.Token,
	}, nil
}

// Path returns the repository path.
func (r *Repository) Path() string {
	return r.path
}

// ConfigUser sets the git user name and email for commits.
func (r *Repository) ConfigUser(name, email string) error {
	if err := r.exec("config", "user.name", name); err != nil {
		return fmt.Errorf("failed to set user.name: %w", err)
	}

	if err := r.exec("config", "user.email", email); err != nil {
		return fmt.Errorf("failed to set user.email: %w", err)
	}

	return nil
}

// Checkout checks out a branch.
func (r *Repository) Checkout(branch string) error {
	if err := r.exec("checkout", branch); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branch, err)
	}
	return nil
}

// CreateBranch creates a new branch.
func (r *Repository) CreateBranch(name string) error {
	if err := r.exec("checkout", "-b", name); err != nil {
		return fmt.Errorf("failed to create branch %s: %w", name, err)
	}
	return nil
}

// Fetch fetches from remote.
func (r *Repository) Fetch(remote string, refspecs ...string) error {
	args := []string{"fetch", remote}
	args = append(args, refspecs...)

	if err := r.exec(args...); err != nil {
		return fmt.Errorf("failed to fetch: %w", err)
	}
	return nil
}

// Push pushes changes to remote.
func (r *Repository) Push(remote, branch string, force bool) error {
	args := []string{"push", remote, branch}

	if force {
		args = append(args, "--force")
	}

	// Build authenticated remote URL if token is provided
	if r.token != "" {
		// Set push URL with authentication
		authRemote := strings.Replace(r.remote, "https://", fmt.Sprintf("https://%s@", r.token), 1)
		if err := r.exec("remote", "set-url", remote, authRemote); err != nil {
			return fmt.Errorf("failed to set authenticated remote: %w", err)
		}
	}

	if err := r.exec(args...); err != nil {
		return fmt.Errorf("failed to push: %w", err)
	}
	return nil
}

// GetCurrentBranch returns the name of the current branch.
func (r *Repository) GetCurrentBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = r.path

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w\nOutput: %s", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}

// GetCommitSHA returns the SHA of a commit reference.
func (r *Repository) GetCommitSHA(ref string) (string, error) {
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = r.path

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get commit SHA for %s: %w\nOutput: %s", ref, err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}

// Clean cleans the working directory.
func (r *Repository) Clean() error {
	if err := r.exec("clean", "-fd"); err != nil {
		return fmt.Errorf("failed to clean working directory: %w", err)
	}
	return nil
}

// Reset performs a hard reset.
func (r *Repository) Reset(ref string) error {
	if err := r.exec("reset", "--hard", ref); err != nil {
		return fmt.Errorf("failed to reset to %s: %w", ref, err)
	}
	return nil
}

// exec executes a git command in the repository directory.
func (r *Repository) exec(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.path

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s failed: %w\nOutput: %s", strings.Join(args, " "), err, string(output))
	}

	return nil
}

// execOutput executes a git command and returns its output.
func (r *Repository) execOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.path

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w\nOutput: %s", strings.Join(args, " "), err, string(output))
	}

	return string(output), nil
}

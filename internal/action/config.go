package action

import (
	"fmt"
	"os"
	"strings"
)

// Config holds the configuration for the PROnto action.
type Config struct {
	// GitHubToken is the authentication token for GitHub API access
	GitHubToken string

	// LabelPattern is the prefix used to identify target branches in labels
	// Example: "pronto/" matches labels like "pronto/release-1.0"
	LabelPattern string

	// ConflictLabel is the label applied to PRs created when cherry-pick conflicts occur
	ConflictLabel string

	// BotName is the git committer name for cherry-pick commits
	BotName string

	// BotEmail is the git committer email for cherry-pick commits
	BotEmail string
}

// LoadConfig reads and validates configuration from GitHub Action inputs.
// GitHub Actions expose inputs as environment variables with the pattern INPUT_<NAME>.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		GitHubToken:   getInput("github_token"),
		LabelPattern:  getInput("label_pattern"),
		ConflictLabel: getInput("conflict_label"),
		BotName:       getInput("bot_name"),
		BotEmail:      getInput("bot_email"),
	}

	// Validate required fields
	if cfg.GitHubToken == "" {
		return nil, fmt.Errorf("github-token is required")
	}

	// Apply defaults for optional fields
	if cfg.LabelPattern == "" {
		cfg.LabelPattern = "pronto/"
	}

	if cfg.ConflictLabel == "" {
		cfg.ConflictLabel = "pronto-conflict"
	}

	if cfg.BotName == "" {
		cfg.BotName = "PROnto Bot"
	}

	if cfg.BotEmail == "" {
		cfg.BotEmail = "pronto[bot]@users.noreply.github.com"
	}

	// Ensure label pattern ends with a separator for clean parsing
	if !strings.HasSuffix(cfg.LabelPattern, "/") {
		cfg.LabelPattern = cfg.LabelPattern + "/"
	}

	return cfg, nil
}

// Validate performs additional validation on the configuration.
func (c *Config) Validate() error {
	if c.GitHubToken == "" {
		return fmt.Errorf("GitHubToken cannot be empty")
	}

	if c.LabelPattern == "" {
		return fmt.Errorf("LabelPattern cannot be empty")
	}

	if c.ConflictLabel == "" {
		return fmt.Errorf("ConflictLabel cannot be empty")
	}

	return nil
}

// getInput retrieves a GitHub Action input from environment variables.
// GitHub Actions expose inputs as INPUT_<UPPERCASED_NAME> environment variables.
func getInput(name string) string {
	// Convert input name to environment variable format
	// "github_token" -> "INPUT_GITHUB_TOKEN"
	envName := "INPUT_" + strings.ToUpper(name)
	return os.Getenv(envName)
}

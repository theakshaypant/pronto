package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds CLI configuration resolved from flags, env, and config files.
type Config struct {
	Token          string `yaml:"token"`
	LabelPattern   string `yaml:"label_pattern"`
	ConflictLabel  string `yaml:"conflict_label"`
	BotName        string `yaml:"bot_name"`
	BotEmail       string `yaml:"bot_email"`
	AlwaysCreatePR bool   `yaml:"always_create_pr"`
}

// Defaults returns a Config with default values.
func Defaults() Config {
	return Config{
		LabelPattern:  "pronto/",
		ConflictLabel: "pronto-conflict",
		BotName:       "PROnto Bot",
		BotEmail:      "pronto[bot]@users.noreply.github.com",
	}
}

// LoadConfig resolves configuration by merging (in priority order):
//  1. Explicit overrides (from CLI flags)
//  2. Environment variables
//  3. .pronto.yml in the current directory (walked up to git root)
//  4. ~/.config/pronto/config.yml
//  5. Built-in defaults
func LoadConfig(overrides Config) (Config, error) {
	cfg := Defaults()

	// Layer 4: global config file
	if home, err := os.UserHomeDir(); err == nil {
		globalPath := filepath.Join(home, ".config", "pronto", "config.yml")
		if fileConfig, err := loadConfigFile(globalPath); err == nil {
			mergeConfig(&cfg, fileConfig)
		}
	}

	// Layer 3: local .pronto.yml (walk up to git root)
	if localPath, err := findLocalConfig(); err == nil {
		if fileConfig, err := loadConfigFile(localPath); err == nil {
			if fileConfig.Token != "" {
				fmt.Fprintf(os.Stderr, "Warning: token found in %s — consider using ~/.config/pronto/config.yml or GITHUB_TOKEN instead to avoid committing secrets\n", localPath)
			}
			mergeConfig(&cfg, fileConfig)
		}
	}

	// Layer 2: environment variables
	mergeEnvConfig(&cfg)

	// Layer 1: CLI flag overrides (highest priority)
	mergeConfig(&cfg, overrides)

	// Ensure label pattern ends with separator
	if cfg.LabelPattern != "" && !strings.HasSuffix(cfg.LabelPattern, "/") {
		cfg.LabelPattern += "/"
	}

	return cfg, nil
}

// ResolveToken returns the token from config, validating it's not empty.
func (c *Config) ResolveToken() (string, error) {
	if c.Token != "" {
		return c.Token, nil
	}
	return "", fmt.Errorf("no GitHub token found. Set GITHUB_TOKEN, use --token, or run 'pronto init'")
}

// loadConfigFile reads and parses a YAML config file.
func loadConfigFile(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid config file %s: %w", path, err)
	}
	return cfg, nil
}

// findLocalConfig walks up from the current directory to the git root
// looking for .pronto.yml.
func findLocalConfig() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		candidate := filepath.Join(dir, ".pronto.yml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		// Stop at git root
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}

	return "", fmt.Errorf(".pronto.yml not found")
}

// mergeEnvConfig applies environment variable values to config.
func mergeEnvConfig(cfg *Config) {
	if v := firstEnv("GITHUB_TOKEN", "GH_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("PRONTO_LABEL_PATTERN"); v != "" {
		cfg.LabelPattern = v
	}
	if v := os.Getenv("PRONTO_CONFLICT_LABEL"); v != "" {
		cfg.ConflictLabel = v
	}
	if v := os.Getenv("PRONTO_BOT_NAME"); v != "" {
		cfg.BotName = v
	}
	if v := os.Getenv("PRONTO_BOT_EMAIL"); v != "" {
		cfg.BotEmail = v
	}
}

// mergeConfig applies non-zero values from src into dst.
func mergeConfig(dst *Config, src Config) {
	if src.Token != "" {
		dst.Token = src.Token
	}
	if src.LabelPattern != "" {
		dst.LabelPattern = src.LabelPattern
	}
	if src.ConflictLabel != "" {
		dst.ConflictLabel = src.ConflictLabel
	}
	if src.BotName != "" {
		dst.BotName = src.BotName
	}
	if src.BotEmail != "" {
		dst.BotEmail = src.BotEmail
	}
	if src.AlwaysCreatePR {
		dst.AlwaysCreatePR = true
	}
}

// firstEnv returns the first non-empty environment variable value.
func firstEnv(keys ...string) string {
	for _, key := range keys {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	if cfg.LabelPattern != "pronto/" {
		t.Errorf("LabelPattern = %q, want %q", cfg.LabelPattern, "pronto/")
	}
	if cfg.ConflictLabel != "pronto-conflict" {
		t.Errorf("ConflictLabel = %q, want %q", cfg.ConflictLabel, "pronto-conflict")
	}
	if cfg.BotName != "PROnto Bot" {
		t.Errorf("BotName = %q, want %q", cfg.BotName, "PROnto Bot")
	}
	if cfg.BotEmail != "pronto[bot]@users.noreply.github.com" {
		t.Errorf("BotEmail = %q, want %q", cfg.BotEmail, "pronto[bot]@users.noreply.github.com")
	}
	if cfg.Token != "" {
		t.Errorf("Token = %q, want empty", cfg.Token)
	}
	if cfg.AlwaysCreatePR {
		t.Errorf("AlwaysCreatePR = true, want false")
	}
}

func TestResolveToken(t *testing.T) {
	t.Run("with token", func(t *testing.T) {
		cfg := Config{Token: "test-token"}
		token, err := cfg.ResolveToken()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "test-token" {
			t.Errorf("token = %q, want %q", token, "test-token")
		}
	})

	t.Run("without token", func(t *testing.T) {
		cfg := Config{}
		_, err := cfg.ResolveToken()
		if err == nil {
			t.Error("expected error for empty token")
		}
	})
}

func TestMergeConfig(t *testing.T) {
	dst := Defaults()
	src := Config{
		Token:   "override-token",
		BotName: "Custom Bot",
	}
	mergeConfig(&dst, src)

	if dst.Token != "override-token" {
		t.Errorf("Token = %q, want %q", dst.Token, "override-token")
	}
	if dst.BotName != "Custom Bot" {
		t.Errorf("BotName = %q, want %q", dst.BotName, "Custom Bot")
	}
	// Unset fields should retain defaults
	if dst.ConflictLabel != "pronto-conflict" {
		t.Errorf("ConflictLabel = %q, want %q", dst.ConflictLabel, "pronto-conflict")
	}
	if dst.LabelPattern != "pronto/" {
		t.Errorf("LabelPattern = %q, want %q", dst.LabelPattern, "pronto/")
	}
}

func TestMergeConfig_AlwaysCreatePR(t *testing.T) {
	dst := Config{AlwaysCreatePR: false}
	src := Config{AlwaysCreatePR: true}
	mergeConfig(&dst, src)

	if !dst.AlwaysCreatePR {
		t.Error("AlwaysCreatePR should be true after merge")
	}
}

// clearProntoEnv unsets all pronto-related env vars for a clean test.
func clearProntoEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{
		"GITHUB_TOKEN", "GH_TOKEN",
		"PRONTO_LABEL_PATTERN", "PRONTO_CONFLICT_LABEL",
		"PRONTO_BOT_NAME", "PRONTO_BOT_EMAIL",
	} {
		t.Setenv(v, "")
	}
}

func TestLoadConfig_EnvOverridesDefaults(t *testing.T) {
	clearProntoEnv(t)
	t.Setenv("GITHUB_TOKEN", "env-token")
	t.Setenv("PRONTO_BOT_NAME", "Env Bot")

	cfg, err := LoadConfig(Config{})
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if cfg.Token != "env-token" {
		t.Errorf("Token = %q, want %q", cfg.Token, "env-token")
	}
	if cfg.BotName != "Env Bot" {
		t.Errorf("BotName = %q, want %q", cfg.BotName, "Env Bot")
	}
}

func TestLoadConfig_FlagsOverrideEnv(t *testing.T) {
	clearProntoEnv(t)
	t.Setenv("GITHUB_TOKEN", "env-token")

	cfg, err := LoadConfig(Config{Token: "flag-token"})
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if cfg.Token != "flag-token" {
		t.Errorf("Token = %q, want %q (flags should override env)", cfg.Token, "flag-token")
	}
}

func TestLoadConfig_GHTokenFallback(t *testing.T) {
	clearProntoEnv(t)
	t.Setenv("GH_TOKEN", "gh-token")

	cfg, err := LoadConfig(Config{})
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if cfg.Token != "gh-token" {
		t.Errorf("Token = %q, want %q", cfg.Token, "gh-token")
	}
}

func TestLoadConfig_GITHUBTokenTakesPrecedence(t *testing.T) {
	clearProntoEnv(t)
	t.Setenv("GITHUB_TOKEN", "github-token")
	t.Setenv("GH_TOKEN", "gh-token")

	cfg, err := LoadConfig(Config{})
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if cfg.Token != "github-token" {
		t.Errorf("Token = %q, want %q (GITHUB_TOKEN should take precedence)", cfg.Token, "github-token")
	}
}

func TestLoadConfig_LabelPatternTrailingSlash(t *testing.T) {
	clearProntoEnv(t)
	t.Setenv("PRONTO_LABEL_PATTERN", "custom-prefix")

	cfg, err := LoadConfig(Config{})
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if cfg.LabelPattern != "custom-prefix/" {
		t.Errorf("LabelPattern = %q, want %q (should auto-add trailing slash)", cfg.LabelPattern, "custom-prefix/")
	}
}

func TestLoadConfig_FileConfig(t *testing.T) {
	clearProntoEnv(t)

	// Create a temp directory with .pronto.yml and .git marker
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	configContent := "bot_name: \"File Bot\"\nconflict_label: \"file-conflict\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".pronto.yml"), []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Change to the temp dir so findLocalConfig picks it up
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	cfg, err := LoadConfig(Config{})
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if cfg.BotName != "File Bot" {
		t.Errorf("BotName = %q, want %q", cfg.BotName, "File Bot")
	}
	if cfg.ConflictLabel != "file-conflict" {
		t.Errorf("ConflictLabel = %q, want %q", cfg.ConflictLabel, "file-conflict")
	}
}

func TestLoadConfig_FlagsOverrideFile(t *testing.T) {
	clearProntoEnv(t)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	configContent := "bot_name: \"File Bot\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".pronto.yml"), []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig(Config{BotName: "Flag Bot"})
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if cfg.BotName != "Flag Bot" {
		t.Errorf("BotName = %q, want %q (flags should override file)", cfg.BotName, "Flag Bot")
	}
}

func TestLoadConfig_EnvOverridesFile(t *testing.T) {
	clearProntoEnv(t)
	t.Setenv("PRONTO_BOT_NAME", "Env Bot")

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	configContent := "bot_name: \"File Bot\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".pronto.yml"), []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig(Config{})
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if cfg.BotName != "Env Bot" {
		t.Errorf("BotName = %q, want %q (env should override file)", cfg.BotName, "Env Bot")
	}
}

func TestFirstEnv(t *testing.T) {
	t.Run("returns first non-empty value", func(t *testing.T) {
		t.Setenv("TEST_FIRST_A", "")
		t.Setenv("TEST_FIRST_B", "value-b")

		result := firstEnv("TEST_FIRST_A", "TEST_FIRST_B")
		if result != "value-b" {
			t.Errorf("firstEnv() = %q, want %q", result, "value-b")
		}
	})

	t.Run("all empty", func(t *testing.T) {
		t.Setenv("TEST_EMPTY_A", "")
		t.Setenv("TEST_EMPTY_B", "")

		result := firstEnv("TEST_EMPTY_A", "TEST_EMPTY_B")
		if result != "" {
			t.Errorf("firstEnv() = %q, want empty", result)
		}
	})

	t.Run("first value wins", func(t *testing.T) {
		t.Setenv("TEST_PRIO_A", "value-a")
		t.Setenv("TEST_PRIO_B", "value-b")

		result := firstEnv("TEST_PRIO_A", "TEST_PRIO_B")
		if result != "value-a" {
			t.Errorf("firstEnv() = %q, want %q", result, "value-a")
		}
	})
}

func TestLoadConfigFile(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yml")
		content := "token: \"my-token\"\nbot_name: \"My Bot\"\nalways_create_pr: true\n"
		os.WriteFile(path, []byte(content), 0o644)

		cfg, err := loadConfigFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Token != "my-token" {
			t.Errorf("Token = %q, want %q", cfg.Token, "my-token")
		}
		if cfg.BotName != "My Bot" {
			t.Errorf("BotName = %q, want %q", cfg.BotName, "My Bot")
		}
		if !cfg.AlwaysCreatePR {
			t.Error("AlwaysCreatePR should be true")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := loadConfigFile("/nonexistent/path/config.yml")
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yml")
		os.WriteFile(path, []byte("{{invalid yaml"), 0o644)

		_, err := loadConfigFile(path)
		if err == nil {
			t.Error("expected error for invalid YAML")
		}
	})
}

package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newInitCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a .pronto.yml config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config file")

	return cmd
}

func runInit(cmd *cobra.Command, force bool) error {
	p := getPrinter(cmd)
	path := ".pronto.yml"

	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", path)
	}

	reader := bufio.NewReader(os.Stdin)
	defaults := Defaults()

	p.Header("PROnto config setup")
	fmt.Println()

	labelPattern := prompt(reader, "Label pattern", defaults.LabelPattern)
	conflictLabel := prompt(reader, "Conflict label", defaults.ConflictLabel)
	botName := prompt(reader, "Bot name", defaults.BotName)
	botEmail := prompt(reader, "Bot email", defaults.BotEmail)

	cfg := Config{
		LabelPattern:  labelPattern,
		ConflictLabel: conflictLabel,
		BotName:       botName,
		BotEmail:      botEmail,
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	fmt.Println()
	p.Success("Created %s", path)
	p.Info("Set GITHUB_TOKEN or add token to ~/.config/pronto/config.yml for authentication.")

	return nil
}

func prompt(reader *bufio.Reader, label, defaultVal string) string {
	fmt.Printf("  %s [%s]: ", label, defaultVal)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

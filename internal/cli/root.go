package cli

import (
	"github.com/spf13/cobra"
)

// Build-time variables injected via ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// NewRootCommand creates the root pronto CLI command.
func NewRootCommand() *cobra.Command {
	var (
		token   string
		verbose bool
	)

	rootCmd := &cobra.Command{
		Use:   "pronto",
		Short: "Get your PR onto release branches, pronto",
		Long: `PROnto CLI — cherry-pick merged PRs to release branches from your terminal.

Auto-detects the repository from your local git remote.
Authenticate with GITHUB_TOKEN, GH_TOKEN, or --token.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Persistent flags available to all subcommands
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "GitHub token (default: $GITHUB_TOKEN or $GH_TOKEN)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	// Add subcommands
	rootCmd.AddCommand(
		newVersionCommand(),
	)

	return rootCmd
}

// resolveConfig builds a Config by merging the root persistent flags with
// env/file/defaults. Call this from subcommand RunE functions.
func resolveConfig(cmd *cobra.Command) (Config, error) {
	token, _ := cmd.Flags().GetString("token")
	overrides := Config{Token: token}
	return LoadConfig(overrides)
}

// getPrinter creates a Printer based on the --verbose flag.
func getPrinter(cmd *cobra.Command) *Printer {
	verbose, _ := cmd.Flags().GetBool("verbose")
	return NewPrinter(verbose)
}

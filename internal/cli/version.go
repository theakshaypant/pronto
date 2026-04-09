package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("pronto %s\n", Version)
			fmt.Printf("  commit: %s\n", Commit)
			fmt.Printf("  built:  %s\n", Date)
		},
	}
}

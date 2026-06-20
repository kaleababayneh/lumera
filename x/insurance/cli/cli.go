
// Package cli provides command-line interfaces for the insurance module.
package cli

import (
	"github.com/spf13/cobra"
)

// GetTxCmd returns the transaction commands for the insurance module
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "insurance",
		Short:                      "Insurance transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	// Add tx commands here
	// cmd.AddCommand(CmdFileClaim())
	// cmd.AddCommand(CmdProcessClaim())
	// etc.

	return cmd
}

// GetQueryCmd returns the cli query commands for the insurance module
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "insurance",
		Short:                      "Insurance query subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	// Add query commands here
	// cmd.AddCommand(CmdQueryPool())
	// cmd.AddCommand(CmdQueryClaim())
	// etc.

	return cmd
}

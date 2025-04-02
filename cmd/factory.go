package cmd

import (
	"fmt"

	"github.com/NahomAnteneh/vec/core"
	"github.com/spf13/cobra"
)

// HandlerFunc is a function type that takes a Repository and arguments
// and returns an error. This is the signature for all command handlers.
type HandlerFunc func(repo *core.Repository, args []string) error

// NewCommand creates a cobra.Command with standard repository handling.
// It automatically finds the repository and passes it to the handler.
func NewCommand(
	use string,
	short string,
	handler HandlerFunc,
	requiredArgs int,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		// Ensure the command has the required number of arguments
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < requiredArgs {
				return fmt.Errorf("requires at least %d argument(s)", requiredArgs)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Find the repository
			repo, err := core.FindRepository()
			if err != nil {
				return fmt.Errorf("failed to find repository: %w", err)
			}
			// Execute the handler
			return handler(repo, args)
		},
	}
	return cmd
}

// NewRepoCommand creates a command that requires a repository.
// This is a simplified version of NewCommand for common cases.
func NewRepoCommand(
	use string,
	short string,
	run func(repo *core.Repository, args []string) error,
) *cobra.Command {
	return NewCommand(use, short, run, 0)
}

// NewInitCommand creates a command that does not require an existing repository.
// This is specifically for commands like 'init' that create a repository.
func NewInitCommand(
	use string,
	short string,
	run func(args []string) error,
) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(args)
		},
	}
}

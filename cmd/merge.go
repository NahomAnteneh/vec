package cmd

import (
	"fmt"

	"github.com/NahomAnteneh/vec/internal/core"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

// mergeCmd defines the "merge" command for the vec CLI.
var mergeCmd = &cobra.Command{
	Use:   "merge <branch>",
	Short: "Join two or more development histories together",
	Long: `Merge integrates changes from the specified branch into the current branch.
It supports fast-forward merges when possible and performs a three-way merge
otherwise, handling conflicts by marking them in the working directory and index.
Resolve conflicts manually and commit the result to complete the merge.`,
	Args: cobra.ExactArgs(1), // Requires exactly one argument: the branch to merge from
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get the repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return fmt.Errorf("failed to find repository root: %w", err)
		}

		// Get the source branch from arguments
		sourceBranch := args[0]

		// Perform the merge
		hasConflicts, err := core.Merge(repoRoot, sourceBranch)
		if err != nil {
			return fmt.Errorf("merge failed: %w", err)
		}

		// Provide feedback based on merge outcome
		if hasConflicts {
			fmt.Println("Merge encountered conflicts. Resolve them and run 'vec commit' to complete the merge.")
		} else {
			// Success message is already printed by core.Merge (e.g., "Fast-forward merge completed" or "Merge completed successfully")
		}

		return nil
	},
}

// init registers the merge command with the root command.
func init() {
	rootCmd.AddCommand(mergeCmd)
}

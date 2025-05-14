package cmd

import (
	"fmt"
	"strings"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/merge"
	"github.com/NahomAnteneh/vec/internal/remote"
	"github.com/spf13/cobra"
)

var (
	mergeStrategy    string
	mergeInteractive bool
	mergeNoCommit    bool
)

// MergeHandler handles the 'merge' command logic
func MergeHandler(repo *core.Repository, args []string) error {
	// Get the branch to merge
	branchName := args[0]

	// Check if this is a remote branch
	if strings.Contains(branchName, "/") {
		parts := strings.SplitN(branchName, "/", 2)
		if len(parts) == 2 {
			remoteName := parts[0]
			remoteBranch := parts[1]

			return remote.MergeRemoteBranchRepo(repo, remoteName, remoteBranch, mergeInteractive)
		}
	}

	// Otherwise, treat as a local branch
	var strategy merge.MergeStrategy
	switch mergeStrategy {
	case "ours":
		strategy = merge.MergeStrategyOurs
	case "theirs":
		strategy = merge.MergeStrategyTheirs
	default:
		strategy = merge.MergeStrategyRecursive
	}

	config := &merge.MergeConfig{
		Strategy:    strategy,
		Interactive: mergeInteractive,
	}

	hasConflicts, err := merge.MergeRepo(repo, branchName, config)
	if err != nil {
		return core.MergeError(fmt.Sprintf("failed to merge branch '%s'", branchName), err)
	}

	if hasConflicts {
		fmt.Println("Merge conflicts detected. Please resolve and commit.")
	}

	return nil
}

func init() {
	mergeCmd := NewRepoCommand(
		"merge [branch-name]",
		"Merge another branch into the current branch",
		MergeHandler,
	)

	mergeCmd.Long = `Merge another branch into the current branch.
This combines the specified branch's history with the current branch.

Examples:
  vec merge feature-branch         # Merge local branch 'feature-branch' into current branch
  vec merge origin/main            # Merge remote branch 'main' from remote 'origin'
  vec merge --strategy=ours topic  # Merge branch 'topic' using the 'ours' strategy`

	mergeCmd.Args = cobra.ExactArgs(1)

	mergeCmd.Flags().StringVar(&mergeStrategy, "strategy", "recursive", "Merge strategy: recursive, ours, or theirs")
	mergeCmd.Flags().BoolVar(&mergeInteractive, "interactive", false, "Resolve conflicts interactively")
	mergeCmd.Flags().BoolVar(&mergeNoCommit, "no-commit", false, "Don't automatically commit the merge")

	rootCmd.AddCommand(mergeCmd)
}

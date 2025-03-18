// cmd/pull.go
package cmd

import (
	"fmt"
	"os"

	"github.com/NahomAnteneh/vec/internal/remote"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

// pullCmd represents the pull command
var pullCmd = &cobra.Command{
	Use:   "pull [<remote>] [<branch>]",
	Short: "Fetch from and integrate with another repository or branch",
	Long: `Fetch from and integrate with another repository or branch.
If no remote is specified, 'origin' is used.
If no branch is specified, the current branch is used.`,
	Args: cobra.RangeArgs(0, 2),
	Run: func(cmd *cobra.Command, args []string) {
		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Printf("Error: %s\n", err)
			os.Exit(1)
		}

		// Determine remote and branch
		remoteName := "origin"
		var branchName string

		if len(args) >= 1 {
			remoteName = args[0]
		}

		if len(args) >= 2 {
			branchName = args[1]
		}

		// Pull from remote
		if err := remote.Pull(repoRoot, remoteName, branchName); err != nil {
			fmt.Printf("Error: %s\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(pullCmd)
}

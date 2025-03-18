// cmd/push.go
package cmd

import (
	"fmt"
	"os"

	"github.com/NahomAnteneh/vec/internal/remote"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

// pushCmd represents the push command
var pushCmd = &cobra.Command{
	Use:   "push [<remote>] [<branch>]",
	Short: "Update remote refs along with associated objects",
	Long: `Update remote refs along with associated objects.
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

		// Check if force flag is set
		force, _ := cmd.Flags().GetBool("force")

		// Push to remote
		if err := remote.Push(repoRoot, remoteName, branchName, force); err != nil {
			fmt.Printf("Error: %s\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(pushCmd)

	// Add flags
	pushCmd.Flags().BoolP("force", "f", false, "Force push even when it results in a non-fast-forward update")
}

// // getRepoRoot finds the repository root by locating the .vec directory
// func getRepoRoot() (string, error) {
//     dir, err := os.Getwd()
//     if err != nil {
//         return "", fmt.Errorf("failed to get current directory: %w", err)
//     }
//     for {
//         if utils.FileExists(filepath.Join(dir, ".vec")) {
//             return dir, nil
//         }
//         parent := filepath.Dir(dir)
//         if parent == dir {
//             return "", fmt.Errorf("not inside a vec repository")
//         }
//         dir = parent
//     }
// }

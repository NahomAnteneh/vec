// cmd/push.go
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/NahomAnteneh/vec/internal/remote"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var (
	// Push command options
	pushForce       bool
	pushDryRun      bool
	pushQuiet       bool
	pushVerbose     bool
	pushAll         bool
	pushUpstream    bool
	pushProgress    bool
	pushTimeout     int
	pushSetUpstream bool
)

// pushCmd represents the push command
var pushCmd = &cobra.Command{
	Use:   "push [<remote>] [<branch>]",
	Short: "Update remote refs along with associated objects",
	Long: `Update remote refs along with associated objects.

Examples:
  vec push                   # Push current branch to default remote (origin)
  vec push upstream          # Push current branch to upstream remote
  vec push origin main       # Push main branch to origin remote
  vec push --all             # Push all branches to default remote
  vec push --force           # Force push (allow non-fast-forward updates)
  vec push --verbose         # Show detailed progress information
  vec push --dry-run         # Simulate push without making changes
  vec push --set-upstream    # Set upstream for current branch

If no remote is specified, 'origin' is used.
If no branch is specified, the current branch is used.`,
	Args: cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}

		// Start time measurement for performance reporting
		startTime := time.Now()

		// Determine remote and branch
		remoteName := "origin"
		var branchName string

		if len(args) >= 1 {
			remoteName = args[0]
		}

		if len(args) >= 2 {
			branchName = args[1]
		}

		// If no branch specified but --all flag is set, push all branches
		if branchName == "" && pushAll {
			if pushVerbose && !pushQuiet {
				fmt.Printf("Pushing all branches to remote '%s'...\n", remoteName)
			}

			// We'll implement this by calling Push for each branch in the repository
			// Get all local branches
			localBranches, err := utils.GetAllBranches(repoRoot)
			if err != nil {
				return fmt.Errorf("failed to get local branches: %w", err)
			}

			if len(localBranches) == 0 {
				return fmt.Errorf("no local branches found to push")
			}

			// Track results for summary
			results := make(map[string]pushResult)
			anySuccess := false

			// Create push options
			pushOptions := remote.PushOptions{
				Force:    pushForce,
				Verbose:  pushVerbose,
				Timeout:  time.Duration(pushTimeout) * time.Second,
				DryRun:   pushDryRun,
				Progress: pushProgress,
			}

			// Push each branch
			for _, branch := range localBranches {
				result := pushResult{
					BranchName: branch,
					Success:    false,
				}

				if !pushQuiet && pushVerbose {
					fmt.Printf("Pushing branch '%s' to '%s'...\n", branch, remoteName)
				}

				err := remote.PushWithOptions(repoRoot, remoteName, branch, pushOptions)
				if err != nil {
					result.Error = err
					if !pushQuiet {
						fmt.Fprintf(os.Stderr, "Error pushing branch '%s': %v\n", branch, err)
					}
				} else {
					result.Success = true
					anySuccess = true
				}

				results[branch] = result
			}

			// Display summary if verbose
			if pushVerbose && !pushQuiet {
				displayPushSummary(results)
			}

			// Show completion timing info
			if !pushQuiet {
				duration := time.Since(startTime).Round(time.Millisecond)
				fmt.Printf("Push completed in %v\n", duration)
			}

			// If no push operations succeeded, return error
			if !anySuccess {
				return fmt.Errorf("failed to push any branches")
			}

			return nil
		}

		// Handle --set-upstream flag
		if pushSetUpstream && branchName == "" {
			// Get current branch if not specified
			currentBranch, err := utils.GetCurrentBranch(repoRoot)
			if err != nil {
				return fmt.Errorf("failed to get current branch: %w", err)
			}

			branchName = currentBranch

			// Set upstream tracking after push
			defer func() {
				if err := utils.SetBranchUpstream(repoRoot, branchName, remoteName); err != nil {
					if !pushQuiet {
						fmt.Fprintf(os.Stderr, "Warning: Failed to set upstream tracking: %v\n", err)
					}
				} else if !pushQuiet {
					fmt.Printf("Branch '%s' set up to track remote branch '%s' from '%s'.\n",
						branchName, branchName, remoteName)
				}
			}()
		}

		// Create push options
		pushOptions := remote.PushOptions{
			Force:    pushForce,
			Verbose:  pushVerbose,
			Timeout:  time.Duration(pushTimeout) * time.Second,
			DryRun:   pushDryRun,
			Progress: pushProgress,
		}

		// Push to remote with options
		if !pushQuiet && !pushOptions.DryRun {
			// Show what we're pushing
			if branchName == "" {
				currentBranch, _ := utils.GetCurrentBranch(repoRoot)
				if currentBranch != "" {
					fmt.Printf("Pushing branch '%s' to '%s'...\n", currentBranch, remoteName)
				} else {
					fmt.Printf("Pushing to '%s'...\n", remoteName)
				}
			} else {
				fmt.Printf("Pushing branch '%s' to '%s'...\n", branchName, remoteName)
			}
		}

		if err := remote.PushWithOptions(repoRoot, remoteName, branchName, pushOptions); err != nil {
			return fmt.Errorf("push failed: %w", err)
		}

		// Show completion timing info if not in quiet mode
		if !pushQuiet {
			duration := time.Since(startTime).Round(time.Millisecond)

			if pushOptions.DryRun {
				fmt.Printf("Dry run completed in %v\n", duration)
			} else {
				fmt.Printf("Push completed in %v\n", duration)
			}
		}

		return nil
	},
}

// Result type for tracking push operations
type pushResult struct {
	BranchName string
	Success    bool
	Error      error
}

// Display a summary of push operations
func displayPushSummary(results map[string]pushResult) {
	fmt.Println("\nPush Summary:")
	fmt.Println("------------------------------------")

	successCount := 0
	failureCount := 0

	for _, result := range results {
		status := "SUCCESS"
		details := ""
		if !result.Success {
			status = "FAILED"
			details = fmt.Sprintf(" - %v", result.Error)
			failureCount++
		} else {
			successCount++
		}
		fmt.Printf("  %s: %s%s\n", result.BranchName, status, details)
	}

	fmt.Println("------------------------------------")
	fmt.Printf("Total: %d successful, %d failed\n", successCount, failureCount)
}

func init() {
	rootCmd.AddCommand(pushCmd)

	// Add push options
	pushCmd.Flags().BoolVarP(&pushForce, "force", "f", false, "Force push even when it results in a non-fast-forward update")
	pushCmd.Flags().BoolVar(&pushDryRun, "dry-run", false, "Simulate push without making changes")
	pushCmd.Flags().BoolVarP(&pushQuiet, "quiet", "q", false, "Suppress all output")
	pushCmd.Flags().BoolVarP(&pushVerbose, "verbose", "v", false, "Be verbose")
	pushCmd.Flags().BoolVar(&pushAll, "all", false, "Push all branches")
	pushCmd.Flags().BoolVarP(&pushUpstream, "upstream", "u", false, "Push to upstream branch instead of remote")
	pushCmd.Flags().BoolVar(&pushProgress, "progress", true, "Show progress during push")
	pushCmd.Flags().IntVar(&pushTimeout, "timeout", 300, "Timeout for push operation in seconds")
	pushCmd.Flags().BoolVar(&pushSetUpstream, "set-upstream", false, "Set upstream for branch")
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

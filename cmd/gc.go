package cmd

import (
	"fmt"

	"github.com/NahomAnteneh/vec/internal/maintenance"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

// gcCmd represents the gc command
var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Clean up unnecessary files from the repository",
	Long: `Garbage collection cleans up unnecessary files from the repository.

This command performs the following task:
1. Finds and removes unreferenced objects that are not pointed to by any commit or branch
2. With the --dry-run option, shows what would be done without making changes

Example:
  vec gc                     # Run garbage collection with default settings
  vec gc -v                  # Run with verbose output
  vec gc -n                  # Dry run (show what would happen without making changes)
`,
	RunE: runGC,
}

var (
	gcDryRun  bool
	gcVerbose bool
)

func init() {
	rootCmd.AddCommand(gcCmd)

	// Add flags
	gcCmd.Flags().BoolVarP(&gcDryRun, "dry-run", "n", false, "Show what would be done without actually removing anything")
	gcCmd.Flags().BoolVarP(&gcVerbose, "verbose", "v", false, "Show detailed information about the garbage collection process")
}

func runGC(cmd *cobra.Command, args []string) error {
	// Find the repository root
	repoRoot, err := utils.GetVecRoot()
	if err != nil {
		return fmt.Errorf("error finding repository: %v", err)
	}

	// Create options for garbage collection
	options := maintenance.GarbageCollectOptions{
		RepoRoot: repoRoot,
		DryRun:   gcDryRun,
		Verbose:  gcVerbose,
	}

	// Run garbage collection
	stats, err := maintenance.GarbageCollect(options)
	if err != nil {
		return fmt.Errorf("garbage collection failed: %v", err)
	}

	// Print summary of the garbage collection process
	if gcDryRun {
		fmt.Println("Dry run: no changes were made")
	}

	fmt.Printf("Garbage collection complete:\n")
	fmt.Printf("- Examined %d objects\n", stats.ObjectsExamined)

	if stats.ObjectsRemoved > 0 || gcDryRun {
		fmt.Printf("- Removed %d unreferenced objects\n", stats.ObjectsRemoved)
	}

	if stats.SpaceSaved > 0 {
		// Convert bytes to a human-readable format
		var unit string
		spaceSaved := float64(stats.SpaceSaved)

		if spaceSaved < 1024 {
			unit = "bytes"
		} else if spaceSaved < 1024*1024 {
			spaceSaved /= 1024
			unit = "KB"
		} else if spaceSaved < 1024*1024*1024 {
			spaceSaved /= (1024 * 1024)
			unit = "MB"
		} else {
			spaceSaved /= (1024 * 1024 * 1024)
			unit = "GB"
		}

		fmt.Printf("- Saved %.2f %s of disk space\n", spaceSaved, unit)
	}

	return nil
}

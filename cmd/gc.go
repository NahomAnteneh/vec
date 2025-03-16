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
	Short: "Clean up unnecessary files and optimize the repository",
	Long: `Garbage collection cleans up unnecessary files and optimizes the local repository.

This command performs several housekeeping tasks:
1. Removes unreferenced objects older than a specified time
2. Optionally packs loose objects into packfiles to save space
3. Optionally prunes old packfiles no longer referenced by the repository
4. With the --dry-run option, shows what would be done without making changes

Example:
  vec gc                   # Run garbage collection with default settings
  vec gc -p                # Run garbage collection and prune old packfiles
  vec gc -a                # Automatically pack loose objects into packfiles
  vec gc -v                # Run with verbose output
  vec gc -n                # Dry run (show what would happen without making changes)
  vec gc -a -p -v          # Full cleanup with verbose output
`,
	RunE: runGC,
}

var (
	gcPrune    bool
	gcAutoPack bool
	gcDryRun   bool
	gcVerbose  bool
)

func init() {
	rootCmd.AddCommand(gcCmd)

	// Add flags
	gcCmd.Flags().BoolVarP(&gcPrune, "prune", "p", false, "Prune loose objects and redundant packfiles")
	gcCmd.Flags().BoolVarP(&gcAutoPack, "auto-pack", "a", false, "Automatically pack loose objects into packfiles")
	gcCmd.Flags().BoolVarP(&gcDryRun, "dry-run", "n", false, "Show what would be removed without actually removing anything")
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
		Prune:    gcPrune,
		AutoPack: gcAutoPack,
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

	if stats.ObjectsPacked > 0 || gcDryRun {
		fmt.Printf("- Packed %d loose objects into packfiles\n", stats.ObjectsPacked)
	}

	if stats.PackfilesPruned > 0 || gcDryRun {
		fmt.Printf("- Pruned %d redundant packfiles\n", stats.PackfilesPruned)
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

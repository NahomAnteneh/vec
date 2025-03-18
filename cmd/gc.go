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
4. Optionally packs referenced objects for better storage efficiency
5. Optionally repacks existing packfiles for better compression
6. With the --dry-run option, shows what would be done without making changes

Example:
  vec gc                     # Run garbage collection with default settings
  vec gc -p                  # Run garbage collection and prune old packfiles
  vec gc -a                  # Automatically pack loose objects into packfiles
  vec gc -v                  # Run with verbose output
  vec gc -n                  # Dry run (show what would happen without making changes)
  vec gc -a -p -v            # Full cleanup with verbose output
  vec gc --pack-all          # Pack both unreferenced and referenced objects
  vec gc --repack            # Repack existing packfiles for better compression
  vec gc --age-threshold=30  # Consider objects older than 30 days as candidates for packing
`,
	RunE: runGC,
}

var (
	gcPrune        bool
	gcAutoPack     bool
	gcDryRun       bool
	gcVerbose      bool
	gcPackAll      bool
	gcRepack       bool
	gcAgeThreshold int
)

func init() {
	rootCmd.AddCommand(gcCmd)

	// Add flags
	gcCmd.Flags().BoolVarP(&gcPrune, "prune", "p", false, "Prune loose objects and redundant packfiles")
	gcCmd.Flags().BoolVarP(&gcAutoPack, "auto-pack", "a", false, "Automatically pack loose objects into packfiles")
	gcCmd.Flags().BoolVarP(&gcDryRun, "dry-run", "n", false, "Show what would be done without actually removing anything")
	gcCmd.Flags().BoolVarP(&gcVerbose, "verbose", "v", false, "Show detailed information about the garbage collection process")
	gcCmd.Flags().BoolVar(&gcPackAll, "pack-all", false, "Pack both unreferenced and referenced objects (more aggressive packing)")
	gcCmd.Flags().BoolVar(&gcRepack, "repack", false, "Repack existing packfiles for better compression")
	gcCmd.Flags().IntVar(&gcAgeThreshold, "age-threshold", 14, "Age threshold in days for considering objects as old enough to pack")
}

func runGC(cmd *cobra.Command, args []string) error {
	// Find the repository root
	repoRoot, err := utils.GetVecRoot()
	if err != nil {
		return fmt.Errorf("error finding repository: %v", err)
	}

	// Create options for garbage collection
	options := maintenance.GarbageCollectOptions{
		RepoRoot:           repoRoot,
		Prune:              gcPrune,
		AutoPack:           gcAutoPack,
		DryRun:             gcDryRun,
		Verbose:            gcVerbose,
		PackAll:            gcPackAll,
		Repack:             gcRepack,
		OldObjectThreshold: gcAgeThreshold,
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

	if stats.ReferencedObjectsPacked > 0 || (gcDryRun && gcPackAll) {
		fmt.Printf("- Packed %d referenced objects\n", stats.ReferencedObjectsPacked)
	}

	if stats.PackfilesRepacked > 0 || (gcDryRun && gcRepack) {
		fmt.Printf("- Repacked %d packfiles\n", stats.PackfilesRepacked)
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

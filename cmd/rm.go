package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var (
	rmForce     bool
	rmCached    bool
	rmRecursive bool
	rmQuiet     bool
)

// rmCmd represents the rm command
var rmCmd = &cobra.Command{
	Use:   "rm [<file>...]",
	Short: "Remove files from the working tree and from the index",
	Long: `Remove files from the working tree and from the index.

The <file> list can include patterns to match multiple files or directories.
If the --cached option is given, the files are only removed from the index, not from the working tree.
If the -r option is given, directories are removed recursively.
If a file is already deleted in the working tree, it will be removed from the index.

Examples:
  vec rm file.txt                 # Remove a single file
  vec rm --cached file.txt        # Remove file from index only
  vec rm -r directory             # Remove directory recursively
  vec rm -f deleted_file.txt      # Remove a deleted file from index`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}

		// Load the index
		index, err := staging.LoadIndex(repoRoot)
		if err != nil {
			return fmt.Errorf("failed to load index: %w", err)
		}

		// Track success status for each path
		success := true
		processedCount := 0

		// Get all tracked files for pattern matching
		trackedFiles := index.GetStagedFiles()

		// Process each path argument
		for _, arg := range args {
			// Handle glob patterns
			matches, err := filepath.Glob(arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid pattern '%s': %v\n", arg, err)
				success = false
				continue
			}

			// If no matches found, treat the argument as a literal path
			if len(matches) == 0 {
				matches = []string{arg}
			}

			for _, match := range matches {
				// Normalize path
				absPath, err := filepath.Abs(match)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error: failed to resolve path '%s': %v\n", match, err)
					success = false
					continue
				}

				relPath, err := filepath.Rel(repoRoot, absPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error: path '%s' is outside repository\n", match)
					success = false
					continue
				}

				// Standardize path separators
				relPath = filepath.ToSlash(relPath)

				// Check if path exists in filesystem
				fileExists := utils.FileExists(absPath)

				// Skip if file doesn't exist and not --cached or --force
				if !fileExists && !rmCached && !rmForce {
					fmt.Fprintf(os.Stderr, "error: '%s' does not exist in the working tree (use --cached or --force to remove from index anyway)\n", relPath)
					success = false
					continue
				}

				// Handle directories
				fileInfo, statErr := os.Stat(absPath)
				isDir := fileExists && statErr == nil && fileInfo.IsDir()

				if isDir {
					if !rmRecursive {
						fmt.Fprintf(os.Stderr, "error: '%s' is a directory, use -r to remove recursively\n", relPath)
						success = false
						continue
					}

					// Process directory recursively
					count, err := removeDirectory(repoRoot, index, absPath, relPath, trackedFiles)
					if err != nil {
						fmt.Fprintf(os.Stderr, "error processing directory '%s': %v\n", relPath, err)
						success = false
					} else {
						processedCount += count
						if !rmQuiet && count > 0 {
							if rmCached {
								fmt.Printf("rm '%s/' (staged): removed %d file(s)\n", relPath, count)
							} else {
								fmt.Printf("rm '%s/': removed %d file(s)\n", relPath, count)
							}
						}
					}
				} else {
					// Handle individual file removal
					if handleFileRemoval(repoRoot, index, relPath, absPath, fileExists) {
						processedCount++
						if !rmQuiet {
							if rmCached {
								fmt.Printf("rm '%s' (staged)\n", relPath)
							} else {
								fmt.Printf("rm '%s'\n", relPath)
							}
						}
					} else {
						success = false
					}
				}
			}
		}

		// Write the updated index back to disk
		if err := index.Write(); err != nil {
			return fmt.Errorf("failed to write index: %w", err)
		}

		if !success {
			return fmt.Errorf("some files could not be removed")
		}

		if processedCount == 0 {
			return fmt.Errorf("no files were removed")
		}

		return nil
	},
}

// removeDirectory handles recursive removal of directories
// Returns the number of files removed and any error encountered
func removeDirectory(repoRoot string, index *staging.Index, absPath, relPath string, trackedFiles []string) (int, error) {
	// Track count of files removed from this directory
	filesRemoved := 0

	// Don't physically remove directories if --cached flag is set
	if !rmCached {
		// Check if we can remove this directory entirely
		entries, err := os.ReadDir(absPath)
		if err != nil {
			return 0, fmt.Errorf("failed to read directory: %w", err)
		}

		// For empty directories, try to remove directly
		if len(entries) == 0 {
			if err := os.Remove(absPath); err != nil {
				return 0, fmt.Errorf("failed to remove empty directory: %w", err)
			}
			return 0, nil
		}
	}

	// Find all tracked files under this directory
	dirPrefix := relPath + "/"
	filesToRemove := []string{}

	for _, trackedFile := range trackedFiles {
		if strings.HasPrefix(trackedFile, dirPrefix) {
			filesToRemove = append(filesToRemove, trackedFile)
		}
	}

	// Handle case where directory exists but contains no tracked files
	if len(filesToRemove) == 0 {
		if rmForce {
			// With force flag, try to remove the directory even if it has no tracked files
			if !rmCached && utils.FileExists(absPath) {
				// Use RemoveAll for empty directories and non-tracked files with -f
				if err := os.RemoveAll(absPath); err != nil {
					return 0, fmt.Errorf("failed to force remove directory: %w", err)
				}
				return 1, nil // Return 1 as we removed something
			}
			return 0, nil
		}

		if !rmCached {
			// Only report error if not in cached mode and trying to remove physical dir
			return 0, fmt.Errorf("directory contains no tracked files (use -f to force removal)")
		}
		return 0, nil
	}

	// Process all files in the directory
	for _, fileRelPath := range filesToRemove {
		fileAbsPath := filepath.Join(repoRoot, fileRelPath)
		fileExists := utils.FileExists(fileAbsPath)

		// Remove from index
		if handleFileRemoval(repoRoot, index, fileRelPath, fileAbsPath, fileExists) {
			filesRemoved++
		}
	}

	// If force flag is set, remove all untracked files from the directory as well
	if rmForce && !rmCached {
		// Walk the directory to remove untracked files
		filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil // Skip errors and directories (we'll clean them up later)
			}

			// Skip the repository directory
			if strings.Contains(path, "/.vec/") {
				return nil
			}

			// Get file's path relative to repo root
			relFilePath, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return nil
			}
			relFilePath = filepath.ToSlash(relFilePath)

			// Check if file is tracked
			_, isTracked := index.GetEntry(relFilePath, 0)
			if !isTracked {
				// Untracked file, remove it
				if err := os.Remove(path); err == nil {
					filesRemoved++
					if !rmQuiet {
						fmt.Printf("rm '%s' (forced)\n", relFilePath)
					}
				}
			}

			return nil
		})
	}

	// If not in cached mode, remove the actual directory structure
	if !rmCached {
		// After removing all files, try to clean up empty directories
		if utils.FileExists(absPath) {
			// Use a more reliable way to cleanup directories
			cleanupEmptyDirs(absPath)
		}
	}

	return filesRemoved, nil
}

// cleanupEmptyDirs recursively removes empty directories
func cleanupEmptyDirs(dirPath string) {
	// Walk directory from bottom to top to ensure we check children first
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return
	}

	// For each subdirectory, recursively clean it
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		subdir := filepath.Join(dirPath, entry.Name())
		cleanupEmptyDirs(subdir)
	}

	// After processing subdirectories, check if this one is now empty
	entries, err = os.ReadDir(dirPath)
	if err != nil {
		return
	}

	if len(entries) == 0 {
		// Directory is empty, remove it
		os.Remove(dirPath)
	}
}

// handleFileRemoval processes a single file removal
func handleFileRemoval(repoRoot string, index *staging.Index, relPath, absPath string, fileExists bool) bool {
	// Check if the file is tracked in the index
	_, isTracked := index.GetEntry(relPath, 0)
	if !isTracked && !rmForce {
		fmt.Fprintf(os.Stderr, "error: '%s' is not tracked by vec\n", relPath)
		return false
	}

	// If --cached, just remove from index without touching filesystem
	if rmCached {
		// Only try to remove from index if it's actually tracked
		if isTracked {
			index.Remove(repoRoot, relPath)
		}
		return true
	}

	// Remove the file from filesystem if it exists and we're not in --cached mode
	if fileExists {
		if err := os.Remove(absPath); err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to remove '%s': %v\n", relPath, err)
			return false
		}
	}

	// Remove from index if tracked
	if isTracked {
		index.Remove(repoRoot, relPath)
	}

	return true
}

func init() {
	rootCmd.AddCommand(rmCmd)

	// Add flags
	rmCmd.Flags().BoolVarP(&rmForce, "force", "f", false, "Override the up-to-date check")
	rmCmd.Flags().BoolVar(&rmCached, "cached", false, "Only remove from the index")
	rmCmd.Flags().BoolVarP(&rmRecursive, "recursive", "r", false, "Allow recursive removal")
	rmCmd.Flags().BoolVarP(&rmQuiet, "quiet", "q", false, "Suppress output")
}

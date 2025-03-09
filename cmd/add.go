package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NahomAnteneh/vec/internal/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

// addCmd defines the "add" command for staging files or directories.
var addCmd = &cobra.Command{
	Use:   "add <file>...",
	Short: "Add file contents to the index",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get the repository root directory
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return fmt.Errorf("failed to find repository root: %w", err)
		}

		// Load the current index (staging area)
		index, err := core.LoadIndex(repoRoot)
		if err != nil {
			return fmt.Errorf("failed to load index: %w", err)
		}

		// Process each argument provided by the user
		for _, arg := range args {
			// Resolve argument to an absolute path
			absPath, err := filepath.Abs(filepath.Join(repoRoot, arg))
			if err != nil {
				return fmt.Errorf("failed to resolve path '%s': %w", arg, err)
			}

			// Check if the path exists
			if _, err := os.Stat(absPath); os.IsNotExist(err) {
				// If it doesn't exist, treat it as a potential glob pattern
				files, err := filepath.Glob(absPath)
				if err != nil || len(files) == 0 {
					fmt.Fprintf(os.Stderr, "warning: pathspec '%s' did not match any files\n", arg)
					continue // Skip to the next argument
				}
				// Add each file matched by the glob pattern
				for _, file := range files {
					if err := addFileOrDir(repoRoot, index, file); err != nil {
						return err
					}
				}
			} else if err != nil {
				return fmt.Errorf("failed to stat '%s': %w", arg, err)
			} else {
				// Path exists, add it directly
				if err := addFileOrDir(repoRoot, index, absPath); err != nil {
					return err
				}
			}
		}

		// Persist the updated index to disk
		if err := index.Write(); err != nil {
			return fmt.Errorf("failed to write index: %w", err)
		}
		return nil
	},
}

// addFileOrDir adds a file or directory (recursively) to the index.
func addFileOrDir(repoRoot string, index *core.Index, absPath string) error {
	// Convert absolute path to relative path for index storage
	relPath, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return fmt.Errorf("failed to get relative path for '%s': %w", absPath, err)
	}

	// Skip ignored files or directories
	if isIgnored, _ := utils.IsIgnored(repoRoot, relPath); isIgnored {
		return nil // Silently skip
	}

	// Get file information
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat '%s': %w", absPath, err)
	}

	if fileInfo.IsDir() {
		// Recursively add all files in the directory
		err := filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return fmt.Errorf("error walking '%s': %w", path, err)
			}
			// Skip directories themselves, only process files
			if info.IsDir() {
				return nil
			}
			return addFileOrDir(repoRoot, index, path)
		})
		if err != nil {
			return fmt.Errorf("failed to walk directory '%s': %w", absPath, err)
		}
	} else {
		// Handle individual file
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("failed to read file '%s': %w", absPath, err)
		}

		// Create a blob object and get its hash
		hash, err := objects.CreateBlob(repoRoot, content)
		if err != nil {
			return fmt.Errorf("failed to create blob for '%s': %w", absPath, err)
		}

		// Add the file to the index
		if err := index.Add(repoRoot, relPath, hash); err != nil {
			return fmt.Errorf("failed to add '%s' to index: %w", relPath, err)
		}
	}
	return nil
}

// init registers the add command with the root command.
func init() {
	rootCmd.AddCommand(addCmd)
}

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

var addCmd = &cobra.Command{
	Use:   "add <file>...",
	Short: "Add file contents to the index",
	Args:  cobra.MinimumNArgs(1), // Require at least one file argument.
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}

		index, err := core.ReadIndex(repoRoot)
		if err != nil {
			return err
		}

		for _, arg := range args {
			// Handle potential glob patterns (e.g., *, ?, []).
			// No change here: Glob is working correctly.
			files, err := filepath.Glob(filepath.Join(repoRoot, arg))
			if err != nil {
				return fmt.Errorf("invalid glob pattern %s: %w", arg, err)
			}

			if len(files) == 0 {
				fmt.Fprintf(os.Stderr, "warning: pathspec '%s' did not match any files\n", arg)
				continue // Skip to the next argument
			}

			for _, file := range files {
				// addFileOrDir now gets the *absolute* path.
				err = addFileOrDir(repoRoot, index, file)
				if err != nil {
					return err
				}
			}
		}

		if err := index.Write(); err != nil {
			return err
		}

		return nil
	},
}

// addFileOrDir adds a file or directory (recursively) to the index.
func addFileOrDir(repoRoot string, index *core.Index, absPath string) error {
	// Check for ignored files/directories.
	if isIgnored, _ := utils.IsIgnored(repoRoot, absPath); isIgnored {
		return nil
	}

	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if fileInfo.IsDir() {
		// Recursively add files within the directory.
		entries, err := os.ReadDir(absPath)
		if err != nil {
			return fmt.Errorf("failed to read directory: %w", err)
		}
		for _, entry := range entries {
			// Recursive call with *absolute* path.
			err = addFileOrDir(repoRoot, index, filepath.Join(absPath, entry.Name()))
			if err != nil {
				return err
			}
		}
	} else {
		// Calculate the *relative* path here, before calling index.Add.
		relPath, err := filepath.Rel(repoRoot, absPath)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Add the file to the index.
		// Add to blob
		content, err := os.ReadFile(absPath) //Read with absolute path
		if err != nil {
			return err
		}
		if _, err := objects.CreateBlob(repoRoot, content); err != nil {
			return err
		}

		if err := index.Add(repoRoot, relPath); err != nil { // Pass relPath to index.Add
			return err // Index.Add already wraps errors.
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(addCmd)
}

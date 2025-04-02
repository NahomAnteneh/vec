package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/spf13/cobra"
)

// AddHandler handles the 'add' command for staging files or directories.
func AddHandler(repo *core.Repository, args []string) error {
	// Load the current index (staging area)
	index, err := staging.LoadIndex(repo.Root)
	if err != nil {
		return core.IndexError("failed to load index", err)
	}

	// Process each argument provided by the user
	for _, arg := range args {
		// Resolve argument to an absolute path
		wd, _ := os.Getwd()
		relPath, _ := filepath.Rel(repo.Root, filepath.Join(wd, arg))
		absPath, err := filepath.Abs(filepath.Join(repo.Root, relPath))
		if err != nil {
			return core.FSError(fmt.Sprintf("failed to resolve path '%s'", arg), err)
		}

		// Check if the path exists
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			// If it doesn't exist, treat it as a potential glob pattern
			files, err := filepath.Glob(absPath)
			if err != nil {
				return core.FSError(fmt.Sprintf("invalid glob pattern '%s'", arg), err)
			}
			if len(files) == 0 {
				fmt.Fprintf(os.Stderr, "warning: pathspec '%s' did not match any files\n", arg)
				continue // Skip to the next argument
			}
			// Add each file matched by the glob pattern
			for _, file := range files {
				if err := addFileOrDir(repo, index, file); err != nil {
					return err
				}
			}
		} else if err != nil {
			return core.FSError(fmt.Sprintf("failed to stat '%s'", arg), err)
		} else {
			// Path exists, add it directly
			if err := addFileOrDir(repo, index, absPath); err != nil {
				return err
			}
		}
	}

	// Persist the updated index to disk
	if err := index.Write(); err != nil {
		return core.IndexError("failed to write index", err)
	}
	return nil
}

// addFileOrDir adds a file or directory (recursively) to the index.
func addFileOrDir(repo *core.Repository, index *staging.Index, absPath string) error {
	// Convert absolute path to relative path for index storage
	relPath, err := filepath.Rel(repo.Root, absPath)
	if err != nil {
		return core.FSError(fmt.Sprintf("failed to get relative path for '%s'", absPath), err)
	}

	// Skip ignored files or directories
	if isIgnored, _ := repo.IsPathIgnored(absPath); isIgnored {
		return nil // Silently skip
	}

	// Get file information
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return core.FSError(fmt.Sprintf("failed to stat '%s'", absPath), err)
	}

	if fileInfo.IsDir() {
		// Recursively add all files in the directory
		err := filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return core.FSError(fmt.Sprintf("error walking '%s'", path), err)
			}
			// Skip directories themselves, only process files
			if info.IsDir() {
				return nil
			}
			return addFileOrDir(repo, index, path)
		})
		if err != nil {
			return core.FSError(fmt.Sprintf("failed to walk directory '%s'", absPath), err)
		}
	} else {
		// Handle individual file
		content, err := os.ReadFile(absPath)
		if err != nil {
			return core.FSError(fmt.Sprintf("failed to read file '%s'", absPath), err)
		}

		// Create a blob object and get its hash
		hash, err := objects.CreateBlob(repo.Root, content)
		if err != nil {
			return core.ObjectError(fmt.Sprintf("failed to create blob for '%s'", absPath), err)
		}

		// Add the file to the index
		if err := index.Add(repo.Root, relPath, hash); err != nil {
			return core.IndexError(fmt.Sprintf("failed to add '%s' to index", relPath), err)
		}
	}
	return nil
}

// init registers the add command with the root command.
func init() {
	addCmd := NewRepoCommand(
		"add <file>...",
		"Add file contents to the index",
		AddHandler,
	)

	// Set minimum args requirement
	addCmd.Args = func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return fmt.Errorf("requires at least 1 argument")
		}
		return nil
	}

	rootCmd.AddCommand(addCmd)
}

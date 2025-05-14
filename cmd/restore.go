package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var (
	restoreSource  string
	restoreStaged  bool
	restoreWorking bool
	restoreQuiet   bool
)

// restoreCmd represents the restore command
var restoreCmd = &cobra.Command{
	Use:   "restore [<file>...]",
	Short: "Restore working tree files or staged content",
	Long: `Restore specified files in the working tree or staging area to a previous state.

By default, the command restores the working tree files from the staging area (index).
With --source, restore files from the specified commit or branch (defaults to HEAD).
With --staged, restore files in the staging area from the HEAD commit.
If no paths are specified, it works on all tracked files.

Examples:
  vec restore file.txt            # Restore file.txt from index to working tree
  vec restore --staged file.txt   # Unstage file.txt (restore from HEAD to index)
  vec restore --source=HEAD~1 file.txt  # Restore file from previous commit
  vec restore --source=main file.txt    # Restore file from main branch
  vec restore .                   # Restore all files in current directory`,
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

		// Default behavior: restore from index to working tree
		// If --source specified: restore from source to working tree
		// If --staged: restore from HEAD/source to index

		// Determine source commit
		var sourceCommitID string
		var sourceTree *objects.TreeObject

		if restoreSource == "" {
			// If no source specified, use HEAD
			sourceCommitID, err = utils.GetHeadCommit(repoRoot)
			if err != nil {
				return fmt.Errorf("failed to get HEAD commit: %w", err)
			}
		} else {
			// Check if source is a branch
			branchPath := filepath.Join(repoRoot, ".vec", "refs", "heads", restoreSource)
			if utils.FileExists(branchPath) {
				commitIDBytes, err := utils.ReadFileContent(branchPath)
				if err != nil {
					return fmt.Errorf("failed to read branch file: %w", err)
				}
				sourceCommitID = strings.TrimSpace(string(commitIDBytes))
			} else {
				// Assume it's a commit ID
				sourceCommitID = restoreSource
			}
		}

		// Load source commit and tree
		if sourceCommitID != "" {
			sourceCommit, err := objects.GetCommit(repoRoot, sourceCommitID)
			if err != nil {
				return fmt.Errorf("invalid source '%s': %w", restoreSource, err)
			}

			sourceTree, err = objects.GetTree(repoRoot, sourceCommit.Tree)
			if err != nil {
				return fmt.Errorf("failed to load tree for commit %s: %w", sourceCommitID, err)
			}
		}

		// If no specific files are provided, use the current directory
		if len(args) == 0 {
			args = []string{"."}
		}

		// Expand file paths and handle patterns
		filesToRestore := []string{}
		for _, arg := range args {
			expanded, err := expandPath(repoRoot, arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: error expanding path '%s': %v\n", arg, err)
				continue
			}
			filesToRestore = append(filesToRestore, expanded...)
		}

		// No files to restore after expansion
		if len(filesToRestore) == 0 {
			return fmt.Errorf("no paths specified for restoration")
		}

		// Handle operations based on flags
		if restoreStaged {
			// Restore staging area from source
			return restoreStageArea(repoRoot, index, sourceTree, filesToRestore)
		} else {
			// Default: restore working tree from index or source
			return restoreWorkingTree(repoRoot, index, sourceTree, filesToRestore)
		}
	},
}

// expandPath expands a path pattern into a list of file paths
func expandPath(repoRoot, path string) ([]string, error) {
	var files []string
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	// Check if the path is a directory
	info, err := os.Stat(absPath)
	if err == nil && info.IsDir() {
		// Walk the directory
		err = filepath.Walk(absPath, func(walkPath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			// Skip directories
			if info.IsDir() {
				return nil
			}
			// Check if file is ignored
			isIgnored, _ := utils.IsIgnored(repoRoot, walkPath)
			if isIgnored {
				return nil
			}
			// Get relative path
			relPath, err := filepath.Rel(repoRoot, walkPath)
			if err != nil {
				return err
			}
			files = append(files, relPath)
			return nil
		})
		return files, err
	}

	// Check if the path is a glob pattern
	matches, err := filepath.Glob(absPath)
	if err != nil {
		// Not a valid glob pattern, treat as a specific file
		relPath, err := filepath.Rel(repoRoot, absPath)
		if err != nil {
			return nil, err
		}
		return []string{relPath}, nil
	}

	// Process glob matches
	for _, match := range matches {
		// Check if the file is ignored
		isIgnored, _ := utils.IsIgnored(repoRoot, match)
		if isIgnored {
			continue
		}
		// Get relative path
		relPath, err := filepath.Rel(repoRoot, match)
		if err != nil {
			continue
		}
		files = append(files, relPath)
	}

	return files, nil
}

// restoreStageArea restores files in the staging area from the source tree
func restoreStageArea(repoRoot string, index *staging.Index, sourceTree *objects.TreeObject, paths []string) error {
	// Collect all files from source tree
	treeFiles := make(map[string]objects.TreeEntry)
	collectTreeEntries(repoRoot, sourceTree, "", treeFiles)

	// Use a map to efficiently check which paths we need to restore
	pathsToRestore := make(map[string]bool)
	for _, path := range paths {
		pathsToRestore[filepath.ToSlash(path)] = true
	}

	// Process each file in the source tree
	modifiedCount := 0
	for treePath, entry := range treeFiles {
		if entry.Type != "blob" {
			continue
		}

		// Check if this file should be restored
		shouldRestore := false
		for pathPattern := range pathsToRestore {
			// Exact match
			if pathPattern == treePath {
				shouldRestore = true
				break
			}
			// Directory match
			if strings.HasSuffix(pathPattern, "/") && strings.HasPrefix(treePath, pathPattern) {
				shouldRestore = true
				break
			}
			// Current directory match
			if pathPattern == "." {
				shouldRestore = true
				break
			}
		}

		if !shouldRestore {
			continue
		}

		// Update the index entry
		entryExists := false
		for i := range index.Entries {
			if index.Entries[i].FilePath == treePath && index.Entries[i].Stage == 0 {
				// Update existing entry
				if index.Entries[i].SHA256 != entry.Hash {
					index.Entries[i].SHA256 = entry.Hash
					modifiedCount++
					if !restoreQuiet {
						fmt.Printf("Restored '%s' in index\n", treePath)
					}
				}
				entryExists = true
				break
			}
		}

		// Add a new entry if it doesn't exist
		if !entryExists {
			// Get file info (we need size and mtime)
			absPath := filepath.Join(repoRoot, treePath)
			var fileInfo os.FileInfo
			var err error

			// Try to get file info, but don't fail if file doesn't exist
			if utils.FileExists(absPath) {
				fileInfo, err = os.Stat(absPath)
				if err != nil {
					return fmt.Errorf("failed to stat file '%s': %w", treePath, err)
				}
			} else {
				// File doesn't exist in working tree, create fake file info
				// This is needed to properly add the entry to the index
				// In a real implementation, you might want to extract the file from the blob
				// and stat it
				blobContent, err := objects.GetBlob(repoRoot, entry.Hash)
				if err != nil {
					return fmt.Errorf("failed to get blob '%s': %w", entry.Hash, err)
				}

				// Create the file temporarily just to get file info
				tempDir, err := os.MkdirTemp("", "vec-restore")
				if err != nil {
					return fmt.Errorf("failed to create temp directory: %w", err)
				}
				defer os.RemoveAll(tempDir)

				tempFile := filepath.Join(tempDir, "temp")
				if err := os.WriteFile(tempFile, blobContent, 0644); err != nil {
					return fmt.Errorf("failed to write temp file: %w", err)
				}

				fileInfo, err = os.Stat(tempFile)
				if err != nil {
					return fmt.Errorf("failed to stat temp file: %w", err)
				}
			}

			// Add to index
			index.Entries = append(index.Entries, staging.IndexEntry{
				Mode:     entry.Mode,
				FilePath: treePath,
				SHA256:   entry.Hash,
				Size:     fileInfo.Size(),
				Mtime:    fileInfo.ModTime(),
				Stage:    0,
			})
			modifiedCount++
			if !restoreQuiet {
				fmt.Printf("Added '%s' to index\n", treePath)
			}
		}
	}

	// Handle file removals from index
	if modifiedCount == 0 {
		for i := 0; i < len(index.Entries); i++ {
			entry := index.Entries[i]
			if entry.Stage != 0 {
				continue
			}

			for pathPattern := range pathsToRestore {
				// Exact match or directory match
				if pathPattern == entry.FilePath ||
					(strings.HasSuffix(pathPattern, "/") && strings.HasPrefix(entry.FilePath, pathPattern)) ||
					pathPattern == "." {
					// Check if this file is in the source tree
					if _, exists := treeFiles[entry.FilePath]; !exists {
						// Not in source tree, remove from index
						index.Entries = append(index.Entries[:i], index.Entries[i+1:]...)
						i--
						modifiedCount++
						if !restoreQuiet {
							fmt.Printf("Removed '%s' from index\n", entry.FilePath)
						}
						break
					}
				}
			}
		}
	}

	// Write the index
	if err := index.Write(); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	if modifiedCount == 0 {
		return fmt.Errorf("no changes to restore in index")
	}

	return nil
}

// restoreWorkingTree restores files in the working tree from index or source tree
func restoreWorkingTree(repoRoot string, index *staging.Index, sourceTree *objects.TreeObject, paths []string) error {
	// Decide source: index (default) or source tree
	useSource := sourceTree != nil && restoreSource != ""

	// Map of paths to restore
	pathsToRestore := make(map[string]bool)
	for _, path := range paths {
		pathsToRestore[filepath.ToSlash(path)] = true
	}

	// Track restored files
	restoredCount := 0

	if useSource {
		// Restore from source tree
		treeFiles := make(map[string]objects.TreeEntry)
		collectTreeEntries(repoRoot, sourceTree, "", treeFiles)

		// Process each file in the source tree
		for treePath, entry := range treeFiles {
			if entry.Type != "blob" {
				continue
			}

			// Check if this file should be restored
			shouldRestore := false
			for pathPattern := range pathsToRestore {
				// Exact match
				if pathPattern == treePath {
					shouldRestore = true
					break
				}
				// Directory match
				if strings.HasSuffix(pathPattern, "/") && strings.HasPrefix(treePath, pathPattern) {
					shouldRestore = true
					break
				}
				// Current directory match
				if pathPattern == "." {
					shouldRestore = true
					break
				}
			}

			if !shouldRestore {
				continue
			}

			// Restore the file
			absPath := filepath.Join(repoRoot, treePath)
			dirPath := filepath.Dir(absPath)

			// Create directory if needed
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory '%s': %w", dirPath, err)
			}

			// Get blob content
			blobContent, err := objects.GetBlob(repoRoot, entry.Hash)
			if err != nil {
				return fmt.Errorf("failed to get blob '%s': %w", entry.Hash, err)
			}

			// Write to file
			if err := os.WriteFile(absPath, blobContent, os.FileMode(entry.Mode)); err != nil {
				return fmt.Errorf("failed to write file '%s': %w", treePath, err)
			}

			restoredCount++
			if !restoreQuiet {
				fmt.Printf("Restored '%s' from '%s'\n", treePath, restoreSource)
			}
		}
	} else {
		// Restore from index
		for _, entry := range index.Entries {
			if entry.Stage != 0 {
				continue
			}

			// Check if this file should be restored
			shouldRestore := false
			for pathPattern := range pathsToRestore {
				// Exact match
				if pathPattern == entry.FilePath {
					shouldRestore = true
					break
				}
				// Directory match
				if strings.HasSuffix(pathPattern, "/") && strings.HasPrefix(entry.FilePath, pathPattern) {
					shouldRestore = true
					break
				}
				// Current directory match
				if pathPattern == "." {
					shouldRestore = true
					break
				}
			}

			if !shouldRestore {
				continue
			}

			// Restore the file
			absPath := filepath.Join(repoRoot, entry.FilePath)
			dirPath := filepath.Dir(absPath)

			// Create directory if needed
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory '%s': %w", dirPath, err)
			}

			// Get blob content
			blobContent, err := objects.GetBlob(repoRoot, entry.SHA256)
			if err != nil {
				return fmt.Errorf("failed to get blob '%s': %w", entry.SHA256, err)
			}

			// Write to file
			if err := os.WriteFile(absPath, blobContent, os.FileMode(entry.Mode)); err != nil {
				return fmt.Errorf("failed to write file '%s': %w", entry.FilePath, err)
			}

			restoredCount++
			if !restoreQuiet {
				fmt.Printf("Restored '%s' from index\n", entry.FilePath)
			}
		}
	}

	if restoredCount == 0 {
		return fmt.Errorf("no files were restored")
	}

	return nil
}

func init() {
	rootCmd.AddCommand(restoreCmd)

	// Add flags
	restoreCmd.Flags().StringVarP(&restoreSource, "source", "s", "", "The source to restore from (commit/branch), defaults to HEAD")
	restoreCmd.Flags().BoolVar(&restoreStaged, "staged", false, "Restore the content in the staging area (unstage)")
	restoreCmd.Flags().BoolVarP(&restoreWorking, "working", "w", false, "Restore the working tree (default behavior)")
	restoreCmd.Flags().BoolVarP(&restoreQuiet, "quiet", "q", false, "Suppress output")
}

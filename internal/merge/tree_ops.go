package merge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
)

// performMerge executes a three-way merge between base, ours, and theirs trees (legacy function).
func performMerge(repoRoot string, index *staging.Index, baseTree, ourTree, theirTree *objects.TreeObject, config *MergeConfig) (MergeResult, error) {
	repo := core.NewRepository(repoRoot)
	return performMergeRepo(repo, index, baseTree, ourTree, theirTree, config)
}

// performMergeRepo executes a three-way merge between base, ours, and theirs trees using Repository context.
func performMergeRepo(repo *core.Repository, index *staging.Index, baseTree, ourTree, theirTree *objects.TreeObject, config *MergeConfig) (MergeResult, error) {
	var result MergeResult
	mergeResults := make(map[string]MergeResult)

	// Build maps for quick lookup.
	baseTreeMap := make(map[string]*objects.TreeEntry)
	ourTreeMap := make(map[string]*objects.TreeEntry)
	theirTreeMap := make(map[string]*objects.TreeEntry)
	for _, entry := range baseTree.Entries {
		baseTreeMap[entry.Name] = &entry
	}
	for _, entry := range ourTree.Entries {
		ourTreeMap[entry.Name] = &entry
	}
	for _, entry := range theirTree.Entries {
		theirTreeMap[entry.Name] = &entry
	}

	// Collect all unique file paths.
	allFilePaths := make(map[string]struct{})
	for path := range baseTreeMap {
		allFilePaths[path] = struct{}{}
	}
	for path := range ourTreeMap {
		allFilePaths[path] = struct{}{}
	}
	for path := range theirTreeMap {
		allFilePaths[path] = struct{}{}
	}

	// Process each file.
	for filePath := range allFilePaths {
		baseEntry, baseExists := baseTreeMap[filePath]
		ourEntry, ourExists := ourTreeMap[filePath]
		theirEntry, theirExists := theirTreeMap[filePath]

		fileResult := MergeResult{
			Path: filePath,
		}

		switch {
		case baseExists && ourExists && theirExists:
			// Unchanged in both?
			if baseEntry.Hash == ourEntry.Hash && baseEntry.Hash == theirEntry.Hash {
				continue
			} else if baseEntry.Hash == ourEntry.Hash {
				// Modified only in theirs.
				if err := copyBlobAndAddToIndex(repo.Root, index, theirEntry.Hash, filePath, theirEntry.Mode); err != nil {
					return MergeResult{}, err
				}
			} else if baseEntry.Hash == theirEntry.Hash {
				// Modified only in ours.
				continue
			} else if ourEntry.Hash == theirEntry.Hash {
				// Modified identically.
				continue
			} else {
				// Conflict: modified differently.
				if err := resolveConflict(repo.Root, index, filePath,
					baseEntry.Hash, ourEntry.Hash, theirEntry.Hash,
					baseEntry.Mode, ourEntry.Mode, theirEntry.Mode,
					config); err != nil {
					return MergeResult{}, err
				}
				result.HasConflicts = true
				fileResult.HasConflicts = true
				fileResult.BaseSha = baseEntry.Hash
				fileResult.OursSha = ourEntry.Hash
				fileResult.TheirsSha = theirEntry.Hash
				mergeResults[filePath] = fileResult
			}

		case baseExists && ourExists && !theirExists:
			// File deleted in theirs.
			if ourEntry.Hash == baseEntry.Hash {
				if err := os.Remove(filepath.Join(repo.Root, filePath)); err != nil && !os.IsNotExist(err) {
					return MergeResult{}, fmt.Errorf("failed to remove file '%s': %w", filePath, err)
				}
				if err := index.Remove(repo.Root, filePath); err != nil {
					return MergeResult{}, err
				}
			} else {
				if err := resolveConflict(repo.Root, index, filePath,
					baseEntry.Hash, ourEntry.Hash, "",
					baseEntry.Mode, ourEntry.Mode, 0,
					config); err != nil {
					return MergeResult{}, err
				}
				result.HasConflicts = true
				fileResult.HasConflicts = true
				fileResult.BaseSha = baseEntry.Hash
				fileResult.OursSha = ourEntry.Hash
				mergeResults[filePath] = fileResult
			}

		case baseExists && !ourExists && theirExists:
			// File deleted in ours.
			if theirEntry.Hash == baseEntry.Hash {
				if err := os.Remove(filepath.Join(repo.Root, filePath)); err != nil && !os.IsNotExist(err) {
					return MergeResult{}, fmt.Errorf("failed to remove file '%s': %w", filePath, err)
				}
				if err := index.Remove(repo.Root, filePath); err != nil {
					return MergeResult{}, err
				}
			} else {
				if err := resolveConflict(repo.Root, index, filePath,
					baseEntry.Hash, "", theirEntry.Hash,
					baseEntry.Mode, 0, theirEntry.Mode,
					config); err != nil {
					return MergeResult{}, err
				}
				result.HasConflicts = true
				fileResult.HasConflicts = true
				fileResult.BaseSha = baseEntry.Hash
				fileResult.TheirsSha = theirEntry.Hash
				mergeResults[filePath] = fileResult
			}

		case !baseExists && ourExists && theirExists:
			// File added in both branches.
			if ourEntry.Hash == theirEntry.Hash {
				continue
			} else {
				if err := resolveConflict(repo.Root, index, filePath,
					"", ourEntry.Hash, theirEntry.Hash,
					0, ourEntry.Mode, theirEntry.Mode,
					config); err != nil {
					return MergeResult{}, err
				}
				result.HasConflicts = true
				fileResult.HasConflicts = true
				fileResult.OursSha = ourEntry.Hash
				fileResult.TheirsSha = theirEntry.Hash
				mergeResults[filePath] = fileResult
			}

		case !baseExists && ourExists && !theirExists:
			// File added only in ours.
			continue

		case !baseExists && !ourExists && theirExists:
			// File added only in theirs.
			if err := copyBlobAndAddToIndex(repo.Root, index, theirEntry.Hash, filePath, theirEntry.Mode); err != nil {
				return MergeResult{}, err
			}

		case baseExists && !ourExists && !theirExists:
			// File deleted in both branches.
			continue
		}
	}

	// Apply any collected merge results, particularly handling binary conflicts
	if len(mergeResults) > 0 {
		if err := applyChangesToWorkingDirectory(repo.Root, mergeResults); err != nil {
			return MergeResult{}, fmt.Errorf("failed to apply merge results: %w", err)
		}
	}

	return result, nil
}

// applyChangesToWorkingDirectory applies merge results to the working directory,
// handling both successful merges and conflicts.
func applyChangesToWorkingDirectory(repoRoot string, mergeResults map[string]MergeResult) error {
	// Iterate through merge results and apply changes
	for path, result := range mergeResults {
		fullPath := filepath.Join(repoRoot, path)

		// Create parent directories if they don't exist
		parentDir := filepath.Dir(fullPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", parentDir, err)
		}

		// Check if file might be binary before applying changes
		if result.HasConflicts && len(result.Content) == 0 && result.ConflictType == "binary" {
			// This is a binary file conflict that couldn't be merged
			// Handle it separately
			if err := handleBinaryConflict(repoRoot, fullPath, result.OursSha, result.TheirsSha); err != nil {
				return fmt.Errorf("failed to handle binary conflict for %s: %w", path, err)
			}
			continue
		}

		// For regular files, write the content
		if result.Content != nil {
			if err := os.WriteFile(fullPath, result.Content, 0644); err != nil {
				return fmt.Errorf("failed to write file %s: %w", fullPath, err)
			}
		}
	}

	return nil
}

// CheckoutCommit updates the working directory and index (legacy function).
func CheckoutCommit(repoRoot, commitID string) error {
	repo := core.NewRepository(repoRoot)
	return CheckoutCommitRepo(repo, commitID)
}

// CheckoutCommitRepo updates the working directory and index to match a commit using Repository context.
func CheckoutCommitRepo(repo *core.Repository, commitID string) error {
	commit, err := objects.GetCommit(repo.Root, commitID)
	if err != nil {
		return fmt.Errorf("failed to load commit %s: %w", commitID, err)
	}
	tree, err := objects.GetTree(repo.Root, commit.Tree)
	if err != nil {
		return fmt.Errorf("failed to load tree %s: %w", commit.Tree, err)
	}
	if err := updateWorkingDirectory(repo.Root, tree, ""); err != nil {
		return fmt.Errorf("failed to update working directory: %w", err)
	}
	index, err := createIndexFromTree(repo.Root, tree, "")
	if err != nil {
		return fmt.Errorf("failed to create index from tree: %w", err)
	}
	if err := index.Write(); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}
	headFile := filepath.Join(repo.Root, ".vec", "HEAD")
	content, err := os.ReadFile(headFile)
	if err != nil {
		return fmt.Errorf("failed to read HEAD file: %w", err)
	}
	if !strings.HasPrefix(string(content), "ref: ") {
		if err := os.WriteFile(headFile, []byte(commitID), 0644); err != nil {
			return fmt.Errorf("failed to update HEAD: %w", err)
		}
	}
	return nil
}

// updateWorkingDirectory updates the working directory to match the tree.
func updateWorkingDirectory(repoRoot string, tree *objects.TreeObject, basePath string) error {
	for _, entry := range tree.Entries {
		currentPath := filepath.Join(basePath, entry.Name)
		absPath := filepath.Join(repoRoot, currentPath)
		if entry.Type == "blob" {
			content, err := objects.GetBlob(repoRoot, entry.Hash)
			if err != nil {
				return fmt.Errorf("failed to get blob '%s': %w", entry.Hash, err)
			}
			if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
				return fmt.Errorf("failed to create directory for '%s': %w", currentPath, err)
			}
			if err := os.WriteFile(absPath, content, 0644); err != nil {
				return fmt.Errorf("failed to write file '%s': %w", currentPath, err)
			}
		} else if entry.Type == "tree" {
			subTree, err := objects.GetTree(repoRoot, entry.Hash)
			if err != nil {
				return fmt.Errorf("failed to get subtree '%s': %w", entry.Hash, err)
			}
			if err := updateWorkingDirectory(repoRoot, subTree, currentPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// createIndexFromTree creates an index from a tree.
func createIndexFromTree(repoRoot string, tree *objects.TreeObject, basePath string) (*staging.Index, error) {
	index := staging.NewIndex(repoRoot)
	for _, entry := range tree.Entries {
		currentPath := filepath.Join(basePath, entry.Name)
		if entry.Type == "blob" {
			absPath := filepath.Join(repoRoot, currentPath)
			stat, err := os.Stat(absPath)
			if err != nil {
				return nil, fmt.Errorf("failed to stat '%s': %w", currentPath, err)
			}
			indexEntry := staging.IndexEntry{
				Mode:     entry.Mode,
				FilePath: currentPath,
				SHA256:   entry.Hash,
				Size:     stat.Size(),
				Mtime:    stat.ModTime(),
				Stage:    0,
			}
			index.Entries = append(index.Entries, indexEntry)
		} else if entry.Type == "tree" {
			subTree, err := objects.GetTree(repoRoot, entry.Hash)
			if err != nil {
				return nil, fmt.Errorf("failed to get subtree '%s': %w", entry.Hash, err)
			}
			subIndex, err := createIndexFromTree(repoRoot, subTree, currentPath)
			if err != nil {
				return nil, err
			}
			index.Entries = append(index.Entries, subIndex.Entries...)
		}
	}
	return index, nil
}

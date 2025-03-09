package merge

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/NahomAnteneh/vec/utils"
)

// MergeResult captures the outcome of the merge operation.
type MergeResult struct {
	HasConflicts bool
}

// Merge performs a merge of the sourceBranch into the current branch in the repository at repoRoot.
// Returns a boolean indicating if there are conflicts (true if conflicts exist) and an error if the operation fails.
func Merge(repoRoot, sourceBranch string) (bool, error) {
	// Validate repository and load index
	if _, err := os.Stat(filepath.Join(repoRoot, ".vec")); os.IsNotExist(err) {
		return false, fmt.Errorf("not a vec repository: %s", repoRoot)
	}
	index, err := staging.LoadIndex(repoRoot)
	if err != nil {
		return false, fmt.Errorf("failed to load index: %w", err)
	}

	// Check for uncommitted changes
	if index.HasUncommittedChanges(repoRoot) {
		return false, fmt.Errorf("uncommitted changes detected; commit or stash them before merging")
	}

	// Load current branch and HEAD
	currentBranch, err := GetCurrentBranch(repoRoot)
	if err != nil {
		return false, fmt.Errorf("failed to determine current branch: %w", err)
	}
	headCommitID, err := utils.ReadHEAD(repoRoot)
	if err != nil {
		return false, fmt.Errorf("failed to read HEAD: %w", err)
	}
	if headCommitID == "" {
		return false, fmt.Errorf("HEAD is not set")
	}

	// Load source branch commit
	sourceBranchFile := filepath.Join(repoRoot, ".vec", "refs", "heads", sourceBranch)
	sourceCommitIDBytes, err := os.ReadFile(sourceBranchFile)
	if err != nil {
		return false, fmt.Errorf("failed to read source branch '%s': %w", sourceBranch, err)
	}
	sourceCommitID := strings.TrimSpace(string(sourceCommitIDBytes))

	// Prevent self-merge
	if currentBranch == sourceBranch {
		return false, fmt.Errorf("cannot merge a branch with itself")
	}

	// Find merge base
	baseCommitID, err := findMergeBase(repoRoot, headCommitID, sourceCommitID)
	if err != nil {
		return false, fmt.Errorf("failed to find merge base: %w", err)
	}

	// Handle fast-forward or already up-to-date cases
	if baseCommitID == headCommitID {
		// Fast-forward: current branch is behind source branch
		if err := CheckoutCommit(repoRoot, sourceCommitID); err != nil {
			return false, fmt.Errorf("failed to checkout source commit for fast-forward: %w", err)
		}
		branchFile := filepath.Join(repoRoot, ".vec", "refs", "heads", currentBranch)
		if err := os.WriteFile(branchFile, []byte(sourceCommitID), 0644); err != nil {
			return false, fmt.Errorf("failed to update branch pointer: %w", err)
		}
		fmt.Println("Fast-forward merge completed.")
		return false, nil
	} else if baseCommitID == sourceCommitID {
		// Already up-to-date: source branch is behind or equal to current branch
		return false, fmt.Errorf("already up-to-date")
	}

	// Load commit objects
	baseCommit, err := objects.GetCommit(repoRoot, baseCommitID)
	if err != nil {
		return false, fmt.Errorf("failed to load base commit: %w", err)
	}
	ourCommit, err := objects.GetCommit(repoRoot, headCommitID)
	if err != nil {
		return false, fmt.Errorf("failed to load our commit: %w", err)
	}
	theirCommit, err := objects.GetCommit(repoRoot, sourceCommitID)
	if err != nil {
		return false, fmt.Errorf("failed to load their commit: %w", err)
	}

	// Load tree objects
	baseTree, err := objects.GetTree(repoRoot, baseCommit.Tree)
	if err != nil {
		return false, fmt.Errorf("failed to load base tree: %w", err)
	}
	ourTree, err := objects.GetTree(repoRoot, ourCommit.Tree)
	if err != nil {
		return false, fmt.Errorf("failed to load our tree: %w", err)
	}
	theirTree, err := objects.GetTree(repoRoot, theirCommit.Tree)
	if err != nil {
		return false, fmt.Errorf("failed to load their tree: %w", err)
	}

	// Perform the three-way merge
	result, err := performMerge(repoRoot, index, baseTree, ourTree, theirTree)
	if err != nil {
		return false, fmt.Errorf("merge failed: %w", err)
	}

	// Write updated index
	if err := index.Write(); err != nil {
		return false, fmt.Errorf("failed to write index: %w", err)
	}

	if result.HasConflicts {
		fmt.Println("Merge conflicts detected. Please resolve them and commit the result.")
		return true, nil
	}

	// Create tree from merged index (implementation needed)
	treeID, err := staging.CreateTreeFromIndex(repoRoot, index)
	if err != nil {
		return false, fmt.Errorf("failed to create tree from index: %w", err)
	}

	// Create merge commit
	author := ourCommit.Author
	committer := ourCommit.Committer // Use committer if available, fallback to author
	if committer == "" {
		committer = author
	}
	message := fmt.Sprintf("Merge branch '%s' into %s", sourceBranch, currentBranch)
	timestamp := time.Now().Unix()
	commitHash, err := objects.CreateCommit(repoRoot, treeID, []string{headCommitID, sourceCommitID}, author, committer, message, timestamp)
	if err != nil {
		return false, fmt.Errorf("failed to create merge commit: %w", err)
	}

	// Update branch pointer
	branchFile := filepath.Join(repoRoot, ".vec", "refs", "heads", currentBranch)
	if err := os.WriteFile(branchFile, []byte(commitHash), 0644); err != nil {
		return false, fmt.Errorf("failed to update branch pointer: %w", err)
	}

	fmt.Println("Merge completed successfully.")
	return false, nil
}

// performMerge executes a three-way merge between base, ours, and theirs trees, updating the index and working directory.
func performMerge(repoRoot string, index *staging.Index, baseTree, ourTree, theirTree *objects.TreeObject) (MergeResult, error) {
	var result MergeResult

	// Build maps for quick lookup
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

	// Collect all unique file paths
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

	// Process each file
	for filePath := range allFilePaths {
		baseEntry, baseExists := baseTreeMap[filePath]
		ourEntry, ourExists := ourTreeMap[filePath]
		theirEntry, theirExists := theirTreeMap[filePath]

		switch {
		case baseExists && ourExists && theirExists:
			// File exists in all three
			if baseEntry.Hash == ourEntry.Hash && baseEntry.Hash == theirEntry.Hash {
				// Unchanged in both
				continue
			} else if baseEntry.Hash == ourEntry.Hash {
				// Modified only in theirs
				if err := copyBlobAndAddToIndex(repoRoot, index, theirEntry.Hash, filePath, theirEntry.Mode); err != nil {
					return MergeResult{}, err
				}
			} else if baseEntry.Hash == theirEntry.Hash {
				// Modified only in ours
				continue // Keep ours (already in working directory and index)
			} else if ourEntry.Hash == theirEntry.Hash {
				// Modified identically in both
				continue // Keep ours
			} else {
				// Conflict: modified differently in both
				if err := handleConflict(repoRoot, index, filePath, baseEntry.Hash, ourEntry.Hash, theirEntry.Hash, baseEntry.Mode, ourEntry.Mode, theirEntry.Mode); err != nil {
					return MergeResult{}, err
				}
				result.HasConflicts = true
			}

		case baseExists && ourExists && !theirExists:
			// File deleted in theirs
			if ourEntry.Hash == baseEntry.Hash {
				// Unchanged in ours, deleted in theirs
				if err := os.Remove(filepath.Join(repoRoot, filePath)); err != nil && !os.IsNotExist(err) {
					return MergeResult{}, fmt.Errorf("failed to remove file '%s': %w", filePath, err)
				}
				if err := index.Remove(repoRoot, filePath); err != nil {
					return MergeResult{}, err
				}
			} else {
				// Conflict: modified in ours, deleted in theirs
				if err := handleConflict(repoRoot, index, filePath, baseEntry.Hash, ourEntry.Hash, "", baseEntry.Mode, ourEntry.Mode, 0); err != nil {
					return MergeResult{}, err
				}
				result.HasConflicts = true
			}

		case baseExists && !ourExists && theirExists:
			// File deleted in ours
			if theirEntry.Hash == baseEntry.Hash {
				// Unchanged in theirs, deleted in ours
				if err := os.Remove(filepath.Join(repoRoot, filePath)); err != nil && !os.IsNotExist(err) {
					return MergeResult{}, fmt.Errorf("failed to remove file '%s': %w", filePath, err)
				}
				if err := index.Remove(repoRoot, filePath); err != nil {
					return MergeResult{}, err
				}
			} else {
				// Conflict: deleted in ours, modified in theirs
				if err := handleConflict(repoRoot, index, filePath, baseEntry.Hash, "", theirEntry.Hash, baseEntry.Mode, 0, theirEntry.Mode); err != nil {
					return MergeResult{}, err
				}
				result.HasConflicts = true
			}

		case !baseExists && ourExists && theirExists:
			// File added in both branches
			if ourEntry.Hash == theirEntry.Hash {
				// Added identically
				continue // Keep ours
			} else {
				// Conflict: added differently
				if err := handleConflict(repoRoot, index, filePath, "", ourEntry.Hash, theirEntry.Hash, 0, ourEntry.Mode, theirEntry.Mode); err != nil {
					return MergeResult{}, err
				}
				result.HasConflicts = true
			}

		case !baseExists && ourExists && !theirExists:
			// File added only in ours
			continue // Keep ours

		case !baseExists && !ourExists && theirExists:
			// File added only in theirs
			if err := copyBlobAndAddToIndex(repoRoot, index, theirEntry.Hash, filePath, theirEntry.Mode); err != nil {
				return MergeResult{}, err
			}

		case baseExists && !ourExists && !theirExists:
			// File deleted in both branches
			continue // Already absent
		}
	}

	return result, nil
}

// handleConflict creates a conflict file in the working directory and updates the index with conflict entries.
func handleConflict(repoRoot string, index *staging.Index, filePath string, baseHash, ourHash, theirHash string, baseMode, ourMode, theirMode int32) error {
	absPath := filepath.Join(repoRoot, filePath)

	// Retrieve content for present versions
	var baseContent, ourContent, theirContent []byte
	if baseHash != "" {
		var err error
		baseContent, err = objects.GetBlob(repoRoot, baseHash)
		if err != nil {
			return fmt.Errorf("failed to get base blob '%s': %w", baseHash, err)
		}
	}
	if ourHash != "" {
		var err error
		ourContent, err = objects.GetBlob(repoRoot, ourHash)
		if err != nil {
			return fmt.Errorf("failed to get our blob '%s': %w", ourHash, err)
		}
	}
	if theirHash != "" {
		var err error
		theirContent, err = objects.GetBlob(repoRoot, theirHash)
		if err != nil {
			return fmt.Errorf("failed to get their blob '%s': %w", theirHash, err)
		}
	}

	// Construct conflict file content
	var conflictContent bytes.Buffer
	if ourHash != "" {
		conflictContent.WriteString("<<<<<<< ours\n")
		conflictContent.Write(ourContent)
		conflictContent.WriteString("\n")
	}
	if baseHash != "" {
		conflictContent.WriteString("||||||| base\n")
		conflictContent.Write(baseContent)
		conflictContent.WriteString("\n")
	}
	conflictContent.WriteString("=======\n")
	if theirHash != "" {
		conflictContent.Write(theirContent)
		conflictContent.WriteString("\n")
	}
	conflictContent.WriteString(">>>>>>> theirs")

	// Determine file mode (default to ours, then theirs, then base, then 100644)
	mode := ourMode
	if mode == 0 {
		mode = theirMode
	}
	if mode == 0 {
		mode = baseMode
	}
	if mode == 0 {
		mode = 100644
	}

	// Write conflict file to working directory
	if err := os.WriteFile(absPath, conflictContent.Bytes(), os.FileMode(mode)); err != nil {
		return fmt.Errorf("failed to write conflict file '%s': %w", filePath, err)
	}

	// Update index with conflict entries
	if err := index.Remove(repoRoot, filePath); err != nil {
		return fmt.Errorf("failed to remove stage 0 entry for '%s': %w", filePath, err)
	}
	if baseHash != "" {
		if err := index.AddConflictEntry(filePath, baseHash, baseMode, 1); err != nil {
			return fmt.Errorf("failed to add base conflict entry for '%s': %w", filePath, err)
		}
	}
	if ourHash != "" {
		if err := index.AddConflictEntry(filePath, ourHash, ourMode, 2); err != nil {
			return fmt.Errorf("failed to add our conflict entry for '%s': %w", filePath, err)
		}
	}
	if theirHash != "" {
		if err := index.AddConflictEntry(filePath, theirHash, theirMode, 3); err != nil {
			return fmt.Errorf("failed to add their conflict entry for '%s': %w", filePath, err)
		}
	}

	return nil
}

// copyBlobAndAddToIndex copies a blob to the working directory and adds it to the index at stage 0.
func copyBlobAndAddToIndex(repoRoot string, index *staging.Index, hash, filePath string, mode int32) error {
	content, err := objects.GetBlob(repoRoot, hash)
	if err != nil {
		return fmt.Errorf("failed to get blob '%s': %w", hash, err)
	}
	absPath := filepath.Join(repoRoot, filePath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for '%s': %w", filePath, err)
	}
	if err := os.WriteFile(absPath, content, os.FileMode(mode)); err != nil {
		return fmt.Errorf("failed to write file '%s': %w", filePath, err)
	}
	if err := index.Add(repoRoot, filePath, hash); err != nil {
		return fmt.Errorf("failed to add '%s' to index: %w", filePath, err)
	}
	return nil
}

// findMergeBase finds the most recent common ancestor of two commits.
func findMergeBase(repoRoot, commit1, commit2 string) (string, error) {
	if commit1 == commit2 {
		return commit1, nil
	}

	visited := make(map[string]bool)
	queue1 := []string{commit1}
	queue2 := []string{commit2}

	for len(queue1) > 0 || len(queue2) > 0 {
		if len(queue1) > 0 {
			current := queue1[0]
			queue1 = queue1[1:]
			if visited[current] {
				continue
			}
			visited[current] = true

			commit, err := objects.GetCommit(repoRoot, current)
			if err != nil {
				return "", fmt.Errorf("failed to load commit %s: %w", current, err)
			}
			for _, parent := range commit.Parents {
				if visited[parent] {
					return parent, nil
				}
				queue1 = append(queue1, parent)
			}
		}

		if len(queue2) > 0 {
			current := queue2[0]
			queue2 = queue2[1:]
			if visited[current] {
				continue
			}
			visited[current] = true

			commit, err := objects.GetCommit(repoRoot, current)
			if err != nil {
				return "", fmt.Errorf("failed to load commit %s: %w", current, err)
			}
			for _, parent := range commit.Parents {
				if visited[parent] {
					return parent, nil
				}
				queue2 = append(queue2, parent)
			}
		}
	}

	return "", fmt.Errorf("no common ancestor found between %s and %s", commit1, commit2)
}

// GetCurrentBranch determines the current branch from HEAD.
func GetCurrentBranch(repoRoot string) (string, error) {
	headFile := filepath.Join(repoRoot, ".vec", "HEAD")
	content, err := os.ReadFile(headFile)
	if err != nil {
		return "", fmt.Errorf("failed to read HEAD file: %w", err)
	}

	ref := strings.TrimSpace(string(content))
	if !strings.HasPrefix(ref, "ref: ") {
		return "", fmt.Errorf("HEAD is not a symbolic reference: %s", ref)
	}

	refPath := strings.TrimSpace(ref[5:])
	parts := strings.Split(refPath, "/")
	if len(parts) != 3 || parts[0] != "refs" || parts[1] != "heads" {
		return "", fmt.Errorf("invalid HEAD reference: %s", ref)
	}

	return parts[2], nil
}

// CheckoutCommit updates the working directory and index to match a commit.
func CheckoutCommit(repoRoot, commitID string) error {
	commit, err := objects.GetCommit(repoRoot, commitID)
	if err != nil {
		return fmt.Errorf("failed to load commit %s: %w", commitID, err)
	}
	tree, err := objects.GetTree(repoRoot, commit.Tree)
	if err != nil {
		return fmt.Errorf("failed to load tree %s: %w", commit.Tree, err)
	}

	if err := updateWorkingDirectory(repoRoot, tree); err != nil {
		return fmt.Errorf("failed to update working directory: %w", err)
	}

	index, err := createIndexFromTree(repoRoot, tree)
	if err != nil {
		return fmt.Errorf("failed to create index from tree: %w", err)
	}
	if err := index.Write(); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	headFile := filepath.Join(repoRoot, ".vec", "HEAD")
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
func updateWorkingDirectory(repoRoot string, tree *objects.TreeObject) error {
	for _, entry := range tree.Entries {
		absPath := filepath.Join(repoRoot, entry.FullPath)
		if entry.Type == "blob" {
			content, err := objects.GetBlob(repoRoot, entry.Hash)
			if err != nil {
				return fmt.Errorf("failed to get blob '%s': %w", entry.Hash, err)
			}
			if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
				return fmt.Errorf("failed to create directory for '%s': %w", entry.FullPath, err)
			}
			if err := os.WriteFile(absPath, content, os.FileMode(entry.Mode)); err != nil {
				return fmt.Errorf("failed to write file '%s': %w", entry.FullPath, err)
			}
		} else if entry.Type == "tree" {
			subTree, err := objects.GetTree(repoRoot, entry.Hash)
			if err != nil {
				return fmt.Errorf("failed to get subtree '%s': %w", entry.Hash, err)
			}
			if err := updateWorkingDirectory(repoRoot, subTree); err != nil {
				return err
			}
		}
	}
	return nil
}

// createIndexFromTree creates an index from a tree.
func createIndexFromTree(repoRoot string, tree *objects.TreeObject) (*staging.Index, error) {
	index := staging.NewIndex(repoRoot)
	for _, entry := range tree.Entries {
		absPath := filepath.Join(repoRoot, entry.FullPath)
		stat, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to stat '%s': %w", entry.FullPath, err)
		}
		indexEntry := staging.IndexEntry{
			Mode:     entry.Mode,
			FilePath: entry.FullPath,
			SHA256:   entry.Hash,
			Size:     stat.Size(),
			Mtime:    stat.ModTime(),
			Stage:    0,
		}
		index.Entries = append(index.Entries, indexEntry)
	}
	return index, nil
}

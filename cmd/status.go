package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the working tree status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}
		return status(repoRoot)
	},
}

func status(repoRoot string) error {
	// Load the index
	index, err := staging.LoadIndex(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to read index: %w", err)
	}

	// Get the HEAD commit and its tree
	headCommitID, err := utils.GetHeadCommit(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	var commitTree *objects.TreeObject
	if headCommitID != "" {
		headCommit, err := objects.GetCommit(repoRoot, headCommitID)
		if err != nil {
			return fmt.Errorf("failed to load HEAD commit: %w", err)
		}
		commitTree, err = objects.GetTree(repoRoot, headCommit.Tree)
		if err != nil {
			return fmt.Errorf("failed to load commit tree: %w", err)
		}
	} else {
		commitTree = objects.NewTreeObject() // Empty tree for new repo
	}

	// Compare states
	newFiles, stagedModified, stagedDeleted, untracked, modifiedNotStaged, deletedNotStaged, err := compareStatus(repoRoot, index, commitTree)
	if err != nil {
		return fmt.Errorf("failed to compare status: %w", err)
	}

	// Get current branch
	branchName, err := utils.GetCurrentBranch(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}
	fmt.Printf("On branch %s\n", branchName)

	// Output "Changes to be committed"
	if len(newFiles) > 0 || len(stagedModified) > 0 || len(stagedDeleted) > 0 {
		fmt.Println("Changes to be committed:")
		fmt.Println("  (use \"vec rm --cached <file>...\" to unstage)")
		fmt.Println()
		sort.Strings(newFiles)
		for _, file := range newFiles {
			fmt.Printf("\tnew file:   %s\n", file)
		}
		sort.Strings(stagedModified)
		for _, file := range stagedModified {
			fmt.Printf("\tmodified:   %s\n", file)
		}
		sort.Strings(stagedDeleted)
		for _, file := range stagedDeleted {
			fmt.Printf("\tdeleted:    %s\n", file)
		}
		fmt.Println()
	}

	// Output "Changes not staged for commit"
	if len(modifiedNotStaged) > 0 || len(deletedNotStaged) > 0 {
		fmt.Println("Changes not staged for commit:")
		fmt.Println("  (use \"vec add <file>...\" to update what will be committed)")
		fmt.Println("  (use \"vec restore <file>...\" to discard changes in working directory)")
		fmt.Println()
		sort.Strings(modifiedNotStaged)
		for _, file := range modifiedNotStaged {
			fmt.Printf("\tmodified:   %s\n", file)
		}
		sort.Strings(deletedNotStaged)
		for _, file := range deletedNotStaged {
			fmt.Printf("\tdeleted:    %s\n", file)
		}
		fmt.Println()
	}

	// Output "Untracked files"
	if len(untracked) > 0 {
		fmt.Println("Untracked files:")
		fmt.Println("  (use \"vec add <file>...\" to include in what will be committed)")
		fmt.Println()
		sort.Strings(untracked)
		for _, file := range untracked {
			fmt.Printf("\t%s\n", file)
		}
		fmt.Println()
	}

	// Output "nothing to commit" if working tree is clean
	if len(newFiles) == 0 && len(stagedModified) == 0 && len(stagedDeleted) == 0 &&
		len(modifiedNotStaged) == 0 && len(deletedNotStaged) == 0 && len(untracked) == 0 {
		fmt.Println("nothing to commit, working tree clean")
	}

	return nil
}

// compareStatus compares the commit tree, index, and working directory.
func compareStatus(repoRoot string, index *staging.Index, commitTree *objects.TreeObject) (
	newFiles, stagedModified, stagedDeleted, untracked, modifiedNotStaged, deletedNotStaged []string, err error) {

	// Build maps for efficient lookup
	indexMap := make(map[string]string) // path -> SHA256
	for _, entry := range index.Entries {
		indexMap[entry.FilePath] = entry.SHA256
	}

	commitTreeMap := make(map[string]objects.TreeEntry) // path -> TreeEntry
	// Updated call: pass repoRoot to buildCommitTreeMap
	buildCommitTreeMap(repoRoot, commitTree, "", commitTreeMap)

	// Track files seen in the working directory to detect deletions
	workingDirFiles := make(map[string]bool)

	// Walk the working directory
	err = filepath.Walk(repoRoot, func(absPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip ignored files and directories
		if isIgnored, _ := utils.IsIgnored(repoRoot, absPath); isIgnored {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(repoRoot, absPath)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}
		workingDirFiles[relPath] = true

		indexHash, inIndex := indexMap[relPath]
		commitEntry, inCommit := commitTreeMap[relPath]
		currentHash, err := utils.HashFile(absPath)
		if err != nil {
			return fmt.Errorf("failed to hash file %s: %w", relPath, err)
		}

		// Case 1: File in all three (commit, index, working dir)
		if inCommit && inIndex {
			if indexHash == commitEntry.Hash {
				if currentHash != indexHash {
					modifiedNotStaged = append(modifiedNotStaged, relPath)
				}
			} else {
				stagedModified = append(stagedModified, relPath)
				if currentHash != indexHash {
					modifiedNotStaged = append(modifiedNotStaged, relPath)
				}
			}
		} else if inIndex && !inCommit { // Case 2: File only in index and working dir (new file)
			newFiles = append(newFiles, relPath)
			if currentHash != indexHash {
				modifiedNotStaged = append(modifiedNotStaged, relPath)
			}
		} else if !inCommit && !inIndex { // Case 3: File only in working dir (untracked)
			untracked = append(untracked, relPath)
		}

		return nil
	})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	// Check for deletions by examining index and commit tree
	for relPath, _ := range commitTreeMap {
		_, inIndex := indexMap[relPath]
		inWorkingDir := workingDirFiles[relPath]

		// Case 4: File in commit but not in index (staged deletion)
		if !inIndex && !inWorkingDir {
			stagedDeleted = append(stagedDeleted, relPath)
		}
	}

	for relPath, _ := range indexMap {
		_, inCommit := commitTreeMap[relPath]
		inWorkingDir := workingDirFiles[relPath]

		// Case 5: File in index but not in working dir (not staged deletion)
		if inCommit && !inWorkingDir {
			deletedNotStaged = append(deletedNotStaged, relPath)
		}
		// Case 6: New file deleted before commit
		if !inCommit && !inWorkingDir {
			// This shouldn't typically happen unless index is corrupted; ignore for now
			fmt.Fprintf(os.Stderr, "Warning: %s in index but not in commit or working dir\n", relPath)
		}
	}

	// Remove duplicates (though logic should prevent them)
	newFiles = removeDuplicates(newFiles)
	stagedModified = removeDuplicates(stagedModified)
	stagedDeleted = removeDuplicates(stagedDeleted)
	untracked = removeDuplicates(untracked)
	modifiedNotStaged = removeDuplicates(modifiedNotStaged)
	deletedNotStaged = removeDuplicates(deletedNotStaged)

	return newFiles, stagedModified, stagedDeleted, untracked, modifiedNotStaged, deletedNotStaged, nil
}

// buildCommitTreeMap recursively builds a map of commit tree entries.
// Added 'repoRoot' so we can properly load subtrees.
func buildCommitTreeMap(repoRoot string, tree *objects.TreeObject, parentPath string, treeMap map[string]objects.TreeEntry) {
	for _, entry := range tree.Entries {
		entryPath := filepath.Join(parentPath, entry.Name)
		if entry.Type == "blob" {
			treeMap[entryPath] = entry
		} else if entry.Type == "tree" {
			subTree, err := objects.GetTree(repoRoot, entry.Hash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting subtree %s: %v\n", entry.Hash, err)
				continue
			}
			buildCommitTreeMap(repoRoot, subTree, entryPath, treeMap)
		}
	}
}

// removeDuplicates removes duplicate strings from a slice.
func removeDuplicates(elements []string) []string {
	encountered := make(map[string]bool)
	var result []string
	for _, elem := range elements {
		if !encountered[elem] {
			encountered[elem] = true
			result = append(result, elem)
		}
	}
	return result
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

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
	index, err := core.ReadIndex(repoRoot)
	if err != nil {
		return err
	}

	headCommitID, err := utils.GetHeadCommit(repoRoot)
	if err != nil {
		return err
	}

	var commitTree *objects.TreeObject
	if headCommitID != "" {
		headCommit, err := objects.GetCommit(repoRoot, headCommitID)
		if err != nil {
			return err
		}

		commitTree, err = objects.GetTree(repoRoot, headCommit.Tree)
		if err != nil {
			return err
		}
	} else {
		commitTree = objects.NewTreeObject()
	}

	// Compare Index to Commit Tree: Find new, modified (staged), and deleted files.
	newFiles, stagedModified, stagedDeleted, untracked, modifiedNotStaged, err := compareIndexAndCommit(repoRoot, index, commitTree)
	if err != nil {
		return err
	}
	// Get up-to-date files
	upToDate := getUpToDateFiles(repoRoot, index, commitTree)

	// Output results
	branchName, err := utils.GetCurrentBranch(repoRoot)
	if err != nil {
		return err
	}
	fmt.Printf("On branch %s\n", branchName)

	if len(newFiles) > 0 || len(stagedModified) > 0 || len(stagedDeleted) > 0 {
		fmt.Println("Changes to be committed:")
		fmt.Println("  (use \"vec rm --cached <file>...\" to unstage)") // rm --cached not implemented yet
		fmt.Println()
		for _, file := range newFiles {
			fmt.Printf("\tnew file:   %s\n", file)
		}
		for _, file := range stagedModified {
			fmt.Printf("\tmodified:   %s\n", file)
		}
		for _, file := range stagedDeleted {
			fmt.Printf("\tdeleted:    %s\n", file)
		}
		fmt.Println()
	}

	if len(modifiedNotStaged) > 0 {
		fmt.Println("Changes not staged for commit:")
		fmt.Println("  (use \"vec add <file>...\" to update what will be committed)")
		fmt.Println("  (use \"vec restore <file>...\" to discard changes in working directory)") // Placeholder
		fmt.Println()
		for _, file := range modifiedNotStaged {
			fmt.Printf("\tmodified:   %s\n", file)
		}
		fmt.Println()
	}

	if len(untracked) > 0 {
		fmt.Println("Untracked files:")
		fmt.Println("  (use \"vec add <file>...\" to include in what will be committed)")
		fmt.Println()
		for _, file := range untracked {
			fmt.Printf("\t%s\n", file)
		}
		fmt.Println()
	}
	// Print up-to-date files only if there are other changes
	if (len(newFiles) > 0 || len(stagedModified) > 0 || len(stagedDeleted) > 0 || len(modifiedNotStaged) > 0 || len(untracked) > 0) && len(upToDate) > 0 {
		fmt.Println("Up-to-date files:")
		fmt.Println()
		for _, file := range upToDate {
			fmt.Printf("\t%s\n", file)
		}
		fmt.Println()
	}

	if len(newFiles) == 0 && len(stagedModified) == 0 && len(stagedDeleted) == 0 && len(modifiedNotStaged) == 0 && len(untracked) == 0 && len(upToDate) == 0 {
		fmt.Println("nothing to commit, working tree clean")
	}
	return nil
}

// compareIndexAndCommit compares the index and the HEAD commit's tree, and the working directory with index.
func compareIndexAndCommit(repoRoot string, index *core.Index, commitTree *objects.TreeObject) (newFiles []string, stagedModified []string, stagedDeleted []string, untracked []string, modifiedNotStaged []string, err error) {
	// Create a map for efficient lookup of commit tree entries.
	commitTreeMap := make(map[string]objects.TreeEntry)
	for _, entry := range commitTree.Entries {
		if entry.Type == "blob" {
			commitTreeMap[entry.Name] = entry
		}
	}

	// Iterate through the *index* to find new, staged modified, and deleted files.
	for _, indexEntry := range index.Entries {
		if commitTreeEntry, ok := commitTreeMap[indexEntry.Filename]; ok {
			// File exists in both index and commit.
			if indexEntry.SHA256 != commitTreeEntry.Hash {
				stagedModified = append(stagedModified, indexEntry.Filename) // Staged modification.
			}
		} else {
			// File exists in index but not in commit -> new file (staged).
			newFiles = append(newFiles, indexEntry.Filename)
		}

		// Check for modified but not staged
		absPath := filepath.Join(repoRoot, indexEntry.Filename)
		currentHash, err := utils.HashFile(absPath)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		if currentHash != indexEntry.SHA256 {
			modifiedNotStaged = append(modifiedNotStaged, indexEntry.Filename)
		}
	}

	// Check for deleted files (files in commit but not in index).
	for commitTreeFilePath, commitTreeEntry := range commitTreeMap {
		if commitTreeEntry.Type != "blob" {
			continue
		}
		foundInIndex := false
		for _, indexEntry := range index.Entries {
			if indexEntry.Filename == commitTreeFilePath {
				foundInIndex = true
				break
			}
		}
		if !foundInIndex {
			// Check if file exists in the working directory
			absPath := filepath.Join(repoRoot, commitTreeFilePath)
			if !utils.FileExists(absPath) {
				stagedDeleted = append(stagedDeleted, commitTreeFilePath) // Deleted file.
			}

		}
	}

	// Find untracked files
	untracked, _ = findUntrackedFiles(repoRoot, index)
	return newFiles, stagedModified, stagedDeleted, untracked, modifiedNotStaged, nil
}

// findUntrackedFiles finds files that are in working directory, but not in index
func findUntrackedFiles(repoRoot string, index *core.Index) ([]string, error) {
	untracked := make([]string, 0)
	err := filepath.Walk(repoRoot, func(absPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err // Abort if we can't walk the directory.
		}

		// Skip the .vec directory and any ignored files
		if isIgnored, _ := utils.IsIgnored(repoRoot, absPath); isIgnored {
			if info.IsDir() {
				return filepath.SkipDir // Skip entire .vec directory.
			}
			return nil // Skip files within .vec.
		}

		if info.IsDir() {
			return nil // Continue traversing directories.
		}

		relPath, _ := filepath.Rel(repoRoot, absPath) // Get path relative to repo root

		found := false
		for _, entry := range index.Entries {
			if entry.Filename == relPath {
				found = true
				break
			}
		}

		if !found {
			untracked = append(untracked, relPath) // Not in index -> untracked.
		}
		return nil
	})
	return untracked, err
}

// getUpToDateFiles finds files that are identical in the working directory, index, and commit tree
func getUpToDateFiles(repoRoot string, index *core.Index, commitTree *objects.TreeObject) []string {
	upToDateFiles := make([]string, 0)

	// Create maps for efficient lookups
	indexMap := make(map[string]string)
	for _, entry := range index.Entries {
		indexMap[entry.Filename] = entry.SHA256
	}

	commitTreeMap := make(map[string]string)
	for _, entry := range commitTree.Entries {
		if entry.Type == "blob" {
			commitTreeMap[entry.Name] = entry.Hash
		}
	}

	// Iterate through files in the working directory
	filepath.Walk(repoRoot, func(absPath string, info os.FileInfo, err error) error {
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
		relPath, _ := filepath.Rel(repoRoot, absPath)

		// Check if the file exists in index and commit and if hashes match
		indexHash, indexExists := indexMap[relPath]
		commitHash, commitExists := commitTreeMap[relPath]

		//The file must present in all to be up-to-date
		if indexExists && commitExists {
			currentHash, err := utils.HashFile(absPath)
			if err != nil {
				return err
			}
			// If all hashes are equal, file is up-to-date
			if currentHash == indexHash && indexHash == commitHash {
				upToDateFiles = append(upToDateFiles, relPath)
			}
		}

		return nil
	})
	return upToDateFiles
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

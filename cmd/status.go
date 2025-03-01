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

	// Get all status information using compareStatus
	newFiles, stagedModified, stagedDeleted, untracked, modifiedNotStaged, upToDate, err := compareStatus(repoRoot, index, commitTree)
	if err != nil {
		return err
	}

	// Output results (Git-like format).
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

// compareStatus compares the index, working directory, and commit tree.
func compareStatus(repoRoot string, index *core.Index, commitTree *objects.TreeObject) (newFiles []string, stagedModified []string, stagedDeleted []string, untracked []string, modifiedNotStaged []string, upToDate []string, err error) {
	// Create maps for efficient lookups (key is relative path).
	indexMap := make(map[string]string)
	for _, entry := range index.Entries {
		relPath, _ := filepath.Rel(repoRoot, entry.Filename)
		indexMap[relPath] = entry.SHA256
	}

	commitTreeMap := make(map[string]objects.TreeEntry)
	buildCommitTreeMap(commitTree, ".", commitTreeMap)

	fmt.Print("----- ", commitTreeMap, "-----\n")

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
		relPath, err := filepath.Rel(repoRoot, absPath)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		indexHash, indexExists := indexMap[relPath]
		commitHash, commitExists := commitTreeMap[relPath]

		currentHash, err := utils.HashFile(absPath)
		if err != nil {
			return err
		}

		if indexExists && commitExists {
			// File exists in index, commit tree, and working directory.
			if indexHash == commitHash.Hash {
				if currentHash == indexHash {
					// Up to date
					upToDate = append(upToDate, relPath)
				} else {
					// Modified, not staged.
					modifiedNotStaged = append(modifiedNotStaged, relPath)
				}
			} else { // indexHash != commitHash
				if currentHash != indexHash {
					// modified and staged
					stagedModified = append(stagedModified, relPath)
					modifiedNotStaged = append(modifiedNotStaged, relPath)
				} else {
					//staged modified
					stagedModified = append(stagedModified, relPath)
				}
			}
		} else if indexExists {
			// File exists only in index -> new file (staged).
			newFiles = append(newFiles, relPath)
			// Check if it is modified
			if currentHash != indexHash {
				modifiedNotStaged = append(modifiedNotStaged, relPath)
			}
		} else if commitExists {
			// File exists in commit tree but not in index
			if !utils.FileExists(absPath) {
				// Deleted
				stagedDeleted = append(stagedDeleted, relPath)
			}
		} else {
			// File exists only in the working directory -> untracked
			untracked = append(untracked, relPath)
		}
		return nil
	})
	// Remove duplication in modifiedNotStaged
	modifiedNotStaged = removeDuplicates(modifiedNotStaged)
	return
}

// buildCommitTreeMap recursively builds a map of commit tree entries.
func buildCommitTreeMap(tree *objects.TreeObject, parentPath string, treeMap map[string]objects.TreeEntry) {
	for _, entry := range tree.Entries {
		entryPath := filepath.Join(parentPath, entry.Name)
		if entry.Type == "blob" {
			treeMap[entryPath] = entry
		} else if entry.Type == "tree" {
			subTree, err := objects.GetTree("", entry.Hash) // Empty repoRoot, as it's not used in GetTree
			fmt.Print("-----", entry.Hash, "-----\n")
			if err != nil {
				// Handle error appropriately, maybe log it and continue
				fmt.Fprintf(os.Stderr, "Error getting subtree: %v\n", err)
				continue
			}
			buildCommitTreeMap(subTree, entry.Name, treeMap) // Recursive call
		}
	}
}

// Helper function to remove duplicate strings from a slice.
func removeDuplicates(elements []string) []string {
	encountered := map[string]bool{}
	result := []string{}

	for v := range elements {
		if encountered[elements[v]] == false {
			encountered[elements[v]] = true
			result = append(result, elements[v])
		}
	}
	return result
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

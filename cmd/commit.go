package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

// commitCmd defines the "commit" command with its usage and flags.
var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Record changes to the repository",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return fmt.Errorf("failed to find repository root: %w", err)
		}
		message, _ := cmd.Flags().GetString("message")
		return commit(repoRoot, message)
	},
}

// commit creates a new commit in the repository.
func commit(repoRoot, message string) error {
	// Load the index to check for staged changes
	index, err := core.LoadIndex(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load index: %w", err)
	}

	// Verify there are changes to commit
	if index.IsClean(repoRoot) {
		return fmt.Errorf("nothing to commit, working tree clean")
	}

	// Retrieve author and committer info from config
	authorName, err := utils.GetConfigValue(repoRoot, "user.name")
	if err != nil || authorName == "" {
		return fmt.Errorf("author name not configured; set it with 'vec config user.name <name>'")
	}
	authorEmail, err := utils.GetConfigValue(repoRoot, "user.email")
	if err != nil || authorEmail == "" {
		return fmt.Errorf("author email not configured; set it with 'vec config user.email <email>'")
	}
	author := fmt.Sprintf("%s <%s>", authorName, authorEmail)
	committer := author // For simplicity, assume committer is the same as author

	// Prompt for commit message if not provided
	if message == "" {
		fmt.Print("Enter commit message: ")
		reader := bufio.NewReader(os.Stdin)
		message, err = reader.ReadString('\n')
		if err != nil || strings.TrimSpace(message) == "" {
			return fmt.Errorf("aborting commit due to empty message")
		}
	}
	message = strings.TrimSpace(message)

	// Get current timestamp
	timestamp := time.Now().Unix()

	// Determine parent commit from HEAD
	parent, err := utils.GetHeadCommit(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to get parent commit: %w", err)
	}
	parents := []string{}
	if parent != "" {
		parents = append(parents, parent)
	}

	// Create tree object from the index
	treeHash, err := createTreeFromIndex(repoRoot, index)
	if err != nil {
		return fmt.Errorf("failed to create tree from index: %w", err)
	}

	// Create the commit object
	commitHash, err := objects.CreateCommit(repoRoot, treeHash, parents, author, committer, message, timestamp)
	if err != nil {
		return fmt.Errorf("failed to create commit: %w", err)
	}

	// Update the branch pointer or HEAD if detached
	branch, err := utils.GetCurrentBranch(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}
	if branch != "(HEAD detached)" {
		branchFile := filepath.Join(repoRoot, ".vec", "refs", "heads", branch)
		if err := os.WriteFile(branchFile, []byte(commitHash), 0644); err != nil {
			return fmt.Errorf("failed to update branch pointer: %w", err)
		}
	} else {
		headFile := filepath.Join(repoRoot, ".vec", "HEAD")
		if err := os.WriteFile(headFile, []byte(commitHash), 0644); err != nil {
			return fmt.Errorf("failed to update HEAD: %w", err)
		}
	}

	// Update reflog with the commit action
	if err := updateReflog(repoRoot, parent, commitHash, branch, "commit", message); err != nil {
		return fmt.Errorf("failed to update reflog: %w", err)
	}

	// Display success message with short commit hash
	fmt.Printf("[(%s) %s] %s\n", branch, commitHash[:7], message)
	return nil
}

// createTreeFromIndex builds a tree object from stage 0 index entries, including subtrees
func createTreeFromIndex(repoRoot string, index *core.Index) (string, error) {
	// Map to group entries by their directory structure
	treeMap := make(map[string][]objects.TreeEntry)

	// Populate treeMap with intermediate directories and file entries
	for _, entry := range index.Entries {
		if entry.Stage != 0 {
			continue // Only process stage 0 (staged changes)
		}
		relPath := entry.FilePath // e.g., "a/b/c.txt"
		parts := strings.Split(relPath, string(filepath.Separator))

		// Create intermediate directory keys
		var curPath string
		for i := range len(parts) - 1 {
			if i == 0 {
				curPath = parts[i]
			} else {
				curPath = filepath.Join(curPath, parts[i])
			}
			if _, ok := treeMap[curPath]; !ok {
				treeMap[curPath] = []objects.TreeEntry{}
			}
		}

		// Add the file blob in its parent directory
		parentPath := strings.Join(parts[:len(parts)-1], string(filepath.Separator))
		fileName := parts[len(parts)-1]
		treeEntry := objects.TreeEntry{
			Mode:     entry.Mode,
			Name:     fileName,
			Hash:     entry.SHA256,
			Type:     "blob",
			FullPath: relPath,
		}
		if parentPath == "" {
			treeMap[""] = append(treeMap[""], treeEntry) // Root-level files
		} else {
			treeMap[parentPath] = append(treeMap[parentPath], treeEntry)
		}
	}

	// Build the tree hierarchy starting from the root
	rootEntries, err := buildTreeHierarchy(repoRoot, "", treeMap)
	if err != nil {
		return "", fmt.Errorf("failed to build tree hierarchy: %w", err)
	}

	// Create the root tree with all entries
	return objects.CreateTreeFromEntries(repoRoot, rootEntries)
}

// buildTreeHierarchy constructs a hierarchical tree structure from the tree map.
func buildTreeHierarchy(repoRoot, dirPath string, treeMap map[string][]objects.TreeEntry) ([]objects.TreeEntry, error) {
	var entries []objects.TreeEntry

	// Add files directly in this directory
	if files, exists := treeMap[dirPath]; exists {
		entries = append(entries, files...)
	}

	// Find immediate child directories by scanning all keys
	subDirs := make(map[string]struct{})
	for path := range treeMap {
		if path == dirPath || !strings.HasPrefix(path, dirPath) {
			continue
		}
		relative := strings.TrimPrefix(path, dirPath)
		if relative == path || relative == "" {
			continue
		}
		relative = strings.TrimPrefix(relative, string(filepath.Separator))
		if relative == "" {
			continue
		}
		parts := strings.SplitN(relative, string(filepath.Separator), 2)
		if len(parts) > 0 {
			subDirs[parts[0]] = struct{}{} // Only immediate children
		}
	}

	// Recursively build subtrees for each subdirectory
	for subDir := range subDirs {
		fullSubDir := subDir
		if dirPath != "" {
			fullSubDir = filepath.Join(dirPath, subDir)
		}
		subEntries, err := buildTreeHierarchy(repoRoot, fullSubDir, treeMap)
		if err != nil {
			return nil, err
		}
		// Create a subtree and get its hash
		subTreeHash, err := objects.CreateTreeFromEntries(repoRoot, subEntries)
		if err != nil {
			return nil, fmt.Errorf("failed to create subtree for '%s': %w", fullSubDir, err)
		}
		entries = append(entries, objects.TreeEntry{
			Mode:     040000, // Directory mode
			Name:     subDir,
			Hash:     subTreeHash,
			Type:     "tree",
			FullPath: fullSubDir,
		})
	}

	// Sort entries lexicographically by name for Git-like consistency
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}

// init registers the commit command and its flags.
func init() {
	commitCmd.Flags().StringP("message", "m", "", "Commit message")
	rootCmd.AddCommand(commitCmd)
}

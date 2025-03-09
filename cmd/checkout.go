package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

// checkoutCmd defines the "checkout" command for switching branches or commits.
var checkoutCmd = &cobra.Command{
	Use:   "checkout <branch>|<commit>",
	Short: "Switch branches or restore working tree files",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return fmt.Errorf("failed to find repository root: %w", err)
		}
		return checkout(repoRoot, args[0])
	},
}

// checkout switches the working directory and index to the specified branch or commit.
func checkout(repoRoot, target string) error {
	// Load index and check for uncommitted changes
	index, err := core.LoadIndex(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load index: %w", err)
	}
	if !index.IsClean(repoRoot) {
		return fmt.Errorf("your local changes would be overwritten by checkout; please commit or stash them first")
	}

	// Determine if target is a branch or commit
	var targetCommitID string
	branchPath := filepath.Join(repoRoot, ".vec", "refs", "heads", target)
	headFile := filepath.Join(repoRoot, ".vec", "HEAD")
	isBranch := utils.FileExists(branchPath)

	if isBranch {
		// Target is a branch
		commitIDBytes, err := os.ReadFile(branchPath)
		if err != nil {
			return fmt.Errorf("failed to read branch file: %w", err)
		}
		targetCommitID = strings.TrimSpace(string(commitIDBytes))
		// Update HEAD to reference the branch
		if err := os.WriteFile(headFile, []byte("ref: refs/heads/"+target), 0644); err != nil {
			return fmt.Errorf("failed to update HEAD to branch: %w", err)
		}
	} else {
		// Target is assumed to be a commit hash (validate later via GetCommit)
		targetCommitID = target
		// Update HEAD to point directly to the commit (detached state)
		if err := os.WriteFile(headFile, []byte(targetCommitID), 0644); err != nil {
			return fmt.Errorf("failed to update HEAD to commit: %w", err)
		}
	}

	// Load and validate the target commit
	targetCommit, err := objects.GetCommit(repoRoot, targetCommitID)
	if err != nil {
		return fmt.Errorf("invalid target '%s': %w", target, err)
	}

	// Load the target tree
	targetTree, err := objects.GetTree(repoRoot, targetCommit.Tree)
	if err != nil {
		return fmt.Errorf("failed to load tree for commit %s: %w", targetCommitID, err)
	}

	// Update working directory and index
	if err := updateWorkingDirectory(repoRoot, targetTree); err != nil {
		return fmt.Errorf("failed to update working directory: %w", err)
	}
	newIndex, err := createIndexFromTree(repoRoot, targetTree)
	if err != nil {
		return fmt.Errorf("failed to update index: %w", err)
	}
	if err := newIndex.Write(); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	// Update reflog
	prevCommitID, _ := utils.GetHeadCommit(repoRoot) // Ignore error, might be initial state
	if err := updateReflog(repoRoot, prevCommitID, targetCommitID, target, "checkout", "moving to "+target); err != nil {
		return fmt.Errorf("failed to update reflog: %w", err)
	}

	fmt.Printf("Switched to %s '%s'\n", map[bool]string{true: "branch", false: "commit"}[isBranch], target)
	return nil
}

// updateWorkingDirectory updates the working directory to match the specified tree.
func updateWorkingDirectory(repoRoot string, tree *objects.TreeObject) error {
	// Build a map of all current files in the working directory
	currentFiles, err := getWorkingDirFiles(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to scan working directory: %w", err)
	}

	// Build a map of tree entries
	treeFiles := make(map[string]objects.TreeEntry)
	collectTreeEntries(tree, "", treeFiles)

	// Update or add files from the tree
	for relPath, entry := range treeFiles {
		if entry.Type != "blob" {
			continue // Skip non-blob entries (subtrees handled recursively)
		}
		absPath := filepath.Join(repoRoot, relPath)
		blobContent, err := objects.GetBlob(repoRoot, entry.Hash)
		if err != nil {
			return fmt.Errorf("failed to get blob %s: %w", entry.Hash, err)
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", relPath, err)
		}
		if err := os.WriteFile(absPath, blobContent, os.FileMode(entry.Mode)); err != nil {
			return fmt.Errorf("failed to write file %s: %w", relPath, err)
		}
		delete(currentFiles, absPath) // Remove from current files as it’s handled
	}

	// Remove files not present in the target tree
	for absPath := range currentFiles {
		if err := os.Remove(absPath); err != nil {
			return fmt.Errorf("failed to remove file %s: %w", absPath, err)
		}
	}

	return nil
}

// getWorkingDirFiles returns a map of all files in the working directory, excluding ignored paths.
func getWorkingDirFiles(repoRoot string) (map[string]struct{}, error) {
	files := make(map[string]struct{})
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path == filepath.Join(repoRoot, ".vec") {
				return filepath.SkipDir
			}
			return nil
		}
		if isIgnored, _ := utils.IsIgnored(repoRoot, path); isIgnored {
			return nil
		}
		files[path] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// collectTreeEntries recursively builds a map of tree entries with full relative paths.
func collectTreeEntries(tree *objects.TreeObject, prefix string, entries map[string]objects.TreeEntry) {
	for _, entry := range tree.Entries {
		fullPath := filepath.Join(prefix, entry.Name)
		if entry.Type == "blob" {
			entries[fullPath] = entry
		} else if entry.Type == "tree" {
			subTree, err := objects.GetTree("", entry.Hash) // Empty repoRoot for relative lookup; adjust if needed
			if err != nil {
				return // Errors handled by caller
			}
			collectTreeEntries(subTree, fullPath, entries)
		}
	}
}

// createIndexFromTree creates a new index from the specified tree.
func createIndexFromTree(repoRoot string, tree *objects.TreeObject) (*core.Index, error) {
	index := core.NewIndex(repoRoot)
	treeFiles := make(map[string]objects.TreeEntry)
	collectTreeEntries(tree, "", treeFiles)

	for relPath, entry := range treeFiles {
		if entry.Type != "blob" {
			continue
		}
		absPath := filepath.Join(repoRoot, relPath)
		fileInfo, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to stat %s: %w", relPath, err)
		}
		index.Entries = append(index.Entries, core.IndexEntry{
			Mode:     entry.Mode,
			FilePath: relPath,
			SHA256:   entry.Hash,
			Size:     fileInfo.Size(),
			Mtime:    fileInfo.ModTime(),
			Stage:    0,
		})
	}
	return index, nil
}

// updateReflog logs an action (e.g., checkout, commit) in the reflog for HEAD and, if applicable, a branch.
func updateReflog(repoRoot, prevCommitID, newCommitID, ref, action, details string) error {
	// Retrieve author/committer info from config
	authorName, err := utils.GetConfigValue(repoRoot, "user.name")
	if err != nil || authorName == "" {
		authorName = "unknown"
	}
	authorEmail, err := utils.GetConfigValue(repoRoot, "user.email")
	if err != nil || authorEmail == "" {
		authorEmail = "unknown"
	}
	author := fmt.Sprintf("%s <%s>", authorName, authorEmail)

	// Handle initial state (no previous commit)
	if prevCommitID == "" {
		prevCommitID = "0000000000000000000000000000000000000000000000000000000000000000" // Zero hash
	}

	// Construct reflog entry
	timestamp := time.Now().Unix()
	var entry string
	if details != "" {
		entry = fmt.Sprintf("%s %s %s %d\t%s: %s\n", prevCommitID, newCommitID, author, timestamp, action, details)
	} else {
		entry = fmt.Sprintf("%s %s %s %d\t%s\n", prevCommitID, newCommitID, author, timestamp, action)
	}

	// Update HEAD reflog
	headReflogPath := filepath.Join(repoRoot, ".vec", "logs", "HEAD")
	f, err := os.OpenFile(headReflogPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to open HEAD reflog at %s: %w", headReflogPath, err)
	}
	defer f.Close()
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("failed to write to HEAD reflog: %w", err)
	}

	// Update branch reflog if ref is a branch
	if ref != "(HEAD detached)" && ref != "" {
		branchReflogPath := filepath.Join(repoRoot, ".vec", "logs", "refs", "heads", ref)
		if err := utils.EnsureDirExists(filepath.Dir(branchReflogPath)); err != nil {
			return fmt.Errorf("failed to create directory for branch reflog %s: %w", branchReflogPath, err)
		}
		fb, err := os.OpenFile(branchReflogPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			return fmt.Errorf("failed to open branch reflog at %s: %w", branchReflogPath, err)
		}
		defer fb.Close()
		if _, err := fb.WriteString(entry); err != nil {
			return fmt.Errorf("failed to write to branch reflog: %w", err)
		}
	}

	return nil
}

// init registers the checkout command with the root command.
func init() {
	rootCmd.AddCommand(checkoutCmd)
}

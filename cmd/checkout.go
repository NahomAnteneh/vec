package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var (
	createBranch  bool
	forceCheckout bool
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
// When the -b flag is passed, it creates a new branch (using shared branch logic)
// and then checks it out.
func checkout(repoRoot, target string) error {
	// Load index and check for uncommitted changes.
	index, err := staging.LoadIndex(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load index: %w", err)
	}
	if !index.IsClean(repoRoot) && !forceCheckout {
		return fmt.Errorf("your local changes would be overwritten by checkout; please commit or stash them first (or use --force to discard changes)")
	}

	branchPath := filepath.Join(repoRoot, ".vec", "refs", "heads", target)
	headFile := filepath.Join(repoRoot, ".vec", "HEAD")
	var targetCommitID string
	var isBranch bool

	if createBranch {
		// Use shared branch creation logic.
		// Make sure the function is exported from branch.go as CreateBranch.
		if err := CreateBranch(repoRoot, target); err != nil {
			return fmt.Errorf("failed to create branch '%s': %w", target, err)
		}
		// Update HEAD to reference the new branch.
		if err := os.WriteFile(headFile, []byte("ref: refs/heads/"+target), 0644); err != nil {
			return fmt.Errorf("failed to update HEAD to branch '%s': %w", target, err)
		}
		// Get the current commit (the branch is created at current HEAD).
		currentCommitID, err := utils.GetHeadCommit(repoRoot)
		if err != nil {
			return fmt.Errorf("failed to get current HEAD commit: %w", err)
		}
		targetCommitID = currentCommitID
		isBranch = true
	} else {
		// Determine if target is an existing branch.
		isBranch = utils.FileExists(branchPath)
		if isBranch {
			commitIDBytes, err := os.ReadFile(branchPath)
			if err != nil {
				return fmt.Errorf("failed to read branch file: %w", err)
			}
			targetCommitID = strings.TrimSpace(string(commitIDBytes))
			// Update HEAD to reference the branch.
			if err := os.WriteFile(headFile, []byte("ref: refs/heads/"+target), 0644); err != nil {
				return fmt.Errorf("failed to update HEAD to branch '%s': %w", target, err)
			}
		} else {
			// Target is assumed to be a commit hash.
			targetCommitID = target
			// Update HEAD to point directly to the commit (detached state).
			if err := os.WriteFile(headFile, []byte(targetCommitID), 0644); err != nil {
				return fmt.Errorf("failed to update HEAD to commit '%s': %w", target, err)
			}
		}
	}

	// Load and validate the target commit.
	targetCommit, err := objects.GetCommit(repoRoot, targetCommitID)
	if err != nil {
		return fmt.Errorf("invalid target '%s': %w", target, err)
	}

	// Load the target tree.
	targetTree, err := objects.GetTree(repoRoot, targetCommit.Tree)
	if err != nil {
		return fmt.Errorf("failed to load tree for commit %s: %w", targetCommitID, err)
	}

	// Update working directory and index.
	if err := updateWorkingDirectory(repoRoot, targetTree, ""); err != nil {
		return fmt.Errorf("failed to update working directory: %w", err)
	}
	newIndex, err := createIndexFromTree(repoRoot, targetTree, "")
	if err != nil {
		return fmt.Errorf("failed to update index: %w", err)
	}
	if err := newIndex.Write(); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	// Update reflog.
	prevCommitID, _ := utils.GetHeadCommit(repoRoot) // May be empty on initial checkout.
	refName := "(HEAD detached)"
	if isBranch {
		refName = target
	}
	if err := updateReflog(repoRoot, prevCommitID, targetCommitID, refName, "checkout", "moving to "+target); err != nil {
		return fmt.Errorf("failed to update reflog: %w", err)
	}

	fmt.Printf("Switched to %s '%s'\n", map[bool]string{true: "branch", false: "commit"}[isBranch], target)
	return nil
}

// The functions updateWorkingDirectory, getWorkingDirFiles, collectTreeEntries,
// collectTreeDirectories, removeExtraDirectories, createIndexFromTree, and updateReflog
// remain unchanged.
func updateWorkingDirectory(repoRoot string, tree *objects.TreeObject, basePath string) error {
	currentFiles, err := getWorkingDirFiles(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to scan working directory: %w", err)
	}

	treeFiles := make(map[string]objects.TreeEntry)
	collectTreeEntries(repoRoot, tree, basePath, treeFiles)

	for relPath, entry := range treeFiles {
		if entry.Type != "blob" {
			continue
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
		delete(currentFiles, relPath)
	}

	for relPath := range currentFiles {
		absPath := filepath.Join(repoRoot, relPath)
		if err := os.RemoveAll(absPath); err != nil {
			return fmt.Errorf("failed to remove file %s: %w", relPath, err)
		}
	}

	validDirs := make(map[string]struct{})
	collectTreeDirectories(repoRoot, tree, basePath, validDirs)
	if err := removeExtraDirectories(repoRoot, validDirs); err != nil {
		return fmt.Errorf("failed to remove extra directories: %w", err)
	}

	return nil
}

func getWorkingDirFiles(repoRoot string) (map[string]struct{}, error) {
	files := make(map[string]struct{})
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && path == filepath.Join(repoRoot, ".vec") {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}
		isIgnored, _ := utils.IsIgnored(repoRoot, path)
		if isIgnored {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		files[rel] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func collectTreeEntries(repoRoot string, tree *objects.TreeObject, prefix string, entries map[string]objects.TreeEntry) {
	for _, entry := range tree.Entries {
		fullPath := filepath.Join(prefix, entry.Name)
		if entry.Type == "blob" {
			entries[fullPath] = entry
		} else if entry.Type == "tree" {
			subTree, err := objects.GetTree(repoRoot, entry.Hash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting subtree %s: %v\n", entry.Hash, err)
				continue
			}
			collectTreeEntries(repoRoot, subTree, fullPath, entries)
		}
	}
}

func collectTreeDirectories(repoRoot string, tree *objects.TreeObject, prefix string, dirs map[string]struct{}) {
	for _, entry := range tree.Entries {
		fullPath := filepath.Join(prefix, entry.Name)
		if entry.Type == "tree" {
			dirs[fullPath] = struct{}{}
			subTree, err := objects.GetTree(repoRoot, entry.Hash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting subtree %s: %v\n", entry.Hash, err)
				continue
			}
			collectTreeDirectories(repoRoot, subTree, fullPath, dirs)
		}
	}
}

func removeExtraDirectories(repoRoot string, validDirs map[string]struct{}) error {
	return filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		if rel == "." || rel == ".vec" {
			return nil
		}
		if _, ok := validDirs[rel]; !ok {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			names, err := f.Readdirnames(1)
			f.Close()
			if err != nil && err != io.EOF {
				return err
			}
			if len(names) == 0 {
				if err := os.Remove(path); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func createIndexFromTree(repoRoot string, tree *objects.TreeObject, basePath string) (*staging.Index, error) {
	index := staging.NewIndex(repoRoot)
	treeFiles := make(map[string]objects.TreeEntry)
	collectTreeEntries(repoRoot, tree, basePath, treeFiles)

	for relPath, entry := range treeFiles {
		if entry.Type != "blob" {
			continue
		}
		absPath := filepath.Join(repoRoot, relPath)
		fileInfo, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to stat %s: %w", relPath, err)
		}
		index.Entries = append(index.Entries, staging.IndexEntry{
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

func updateReflog(repoRoot, prevCommitID, newCommitID, ref, action, details string) error {
	authorName, err := utils.GetConfigValue(repoRoot, "user.name")
	if err != nil || authorName == "" {
		authorName = "unknown"
	}
	authorEmail, err := utils.GetConfigValue(repoRoot, "user.email")
	if err != nil || authorEmail == "" {
		authorEmail = "unknown"
	}
	author := fmt.Sprintf("%s <%s>", authorName, authorEmail)

	if prevCommitID == "" {
		prevCommitID = "0000000000000000000000000000000000000000000000000000000000000000"
	}

	timestamp := time.Now().Unix()
	var entry string
	if details != "" {
		entry = fmt.Sprintf("%s %s %s %d\t%s: %s\n", prevCommitID, newCommitID, author, timestamp, action, details)
	} else {
		entry = fmt.Sprintf("%s %s %s %d\t%s\n", prevCommitID, newCommitID, author, timestamp, action)
	}

	headReflogPath := filepath.Join(repoRoot, ".vec", "logs", "HEAD")
	f, err := os.OpenFile(headReflogPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to open HEAD reflog at %s: %w", headReflogPath, err)
	}
	defer f.Close()
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("failed to write to HEAD reflog: %w", err)
	}

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

func init() {
	rootCmd.AddCommand(checkoutCmd)
	checkoutCmd.Flags().BoolVarP(&createBranch, "branch", "b", false, "Create a new branch and switch to it")
	checkoutCmd.Flags().BoolVarP(&forceCheckout, "force", "f", false, "Discard local changes and switch to the specified branch or commit")
}

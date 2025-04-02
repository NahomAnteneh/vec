package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var (
	createBranch  bool
	forceCheckout bool
)

// CheckoutHandler handles the checkout command logic using the repository context
func CheckoutHandler(repo *core.Repository, args []string) error {
	return checkoutRepo(repo, args[0])
}

// checkoutRepo switches the working directory and index to the specified branch or commit.
// When the -b flag is passed, it creates a new branch (using shared branch logic)
// and then checks it out.
func checkoutRepo(repo *core.Repository, target string) error {
	// Load index and check for uncommitted changes.
	index, err := staging.LoadIndexRepo(repo)
	if err != nil {
		return core.IndexError("failed to load index", err)
	}
	if !index.IsCleanRepo(repo) && !forceCheckout {
		return core.RepositoryError("your local changes would be overwritten by checkout; please commit or stash them first (or use --force to discard changes)", nil)
	}

	branchPath := filepath.Join(repo.VecDir, "refs", "heads", target)
	headFile := filepath.Join(repo.VecDir, "HEAD")
	var targetCommitID string
	var isBranch bool

	if createBranch {
		// Use shared branch creation logic.
		if err := CreateBranch(repo, target); err != nil {
			return core.RefError(fmt.Sprintf("failed to create branch '%s'", target), err)
		}
		// Update HEAD to reference the new branch.
		if err := os.WriteFile(headFile, []byte("ref: refs/heads/"+target), 0644); err != nil {
			return core.RefError(fmt.Sprintf("failed to update HEAD to branch '%s'", target), err)
		}
		// Get the current commit (the branch is created at current HEAD).
		currentCommitID, err := utils.GetHeadCommit(repo.Root)
		if err != nil {
			return core.RefError("failed to get current HEAD commit", err)
		}
		targetCommitID = currentCommitID
		isBranch = true
	} else {
		// First, check if target is an existing branch
		isBranch = utils.FileExists(branchPath)
		if isBranch {
			// Target is a branch, read its commit ID
			commitIDBytes, err := os.ReadFile(branchPath)
			if err != nil {
				return core.RefError("failed to read branch file", err)
			}
			targetCommitID = strings.TrimSpace(string(commitIDBytes))
			// Update HEAD to reference the branch.
			if err := os.WriteFile(headFile, []byte("ref: refs/heads/"+target), 0644); err != nil {
				return core.RefError(fmt.Sprintf("failed to update HEAD to branch '%s'", target), err)
			}
		} else {
			// Target might be a commit hash (full or partial)
			// Check if it looks like a hash (contains only hexadecimal digits)
			isHex := true
			for _, c := range target {
				if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
					isHex = false
					break
				}
			}

			if isHex && len(target) >= 4 {
				// Try to resolve it as a partial hash
				fullHash, err := utils.FindObjectByPartialHash(repo.Root, target)
				if err != nil {
					return core.ObjectError(fmt.Sprintf("failed to resolve '%s' as commit", target), err)
				}

				// Verify it's a commit object
				_, err = objects.GetCommit(repo.Root, fullHash)
				if err != nil {
					return core.ObjectError(fmt.Sprintf("object '%s' exists but is not a commit", fullHash), err)
				}

				targetCommitID = fullHash
				// Update HEAD to point directly to the commit (detached state)
				if err := os.WriteFile(headFile, []byte(targetCommitID), 0644); err != nil {
					return core.RefError(fmt.Sprintf("failed to update HEAD to commit '%s'", target), err)
				}
			} else {
				// Not a valid branch or commit hash format
				return core.RefError(fmt.Sprintf("'%s' is not a valid branch name or commit hash", target), nil)
			}
		}
	}

	// Load and validate the target commit.
	targetCommit, err := objects.GetCommit(repo.Root, targetCommitID)
	if err != nil {
		return core.ObjectError(fmt.Sprintf("invalid target '%s'", target), err)
	}

	// Load the target tree.
	targetTree, err := objects.GetTree(repo.Root, targetCommit.Tree)
	if err != nil {
		return core.ObjectError(fmt.Sprintf("failed to load tree for commit %s", targetCommitID), err)
	}

	// Update working directory and index.
	if err := updateWorkingDirectoryRepo(repo, targetTree, ""); err != nil {
		return core.FSError("failed to update working directory", err)
	}
	newIndex, err := createIndexFromTreeRepo(repo, targetTree, "")
	if err != nil {
		return core.IndexError("failed to update index", err)
	}
	if err := newIndex.Write(); err != nil {
		return core.FSError("failed to write index", err)
	}

	// Update reflog.
	prevCommitID, _ := utils.GetHeadCommit(repo.Root) // May be empty on initial checkout.
	refName := "(HEAD detached)"
	if isBranch {
		refName = target
	}
	if err := updateReflogForCheckoutRepo(repo, prevCommitID, targetCommitID, refName, "checkout", "moving to "+target); err != nil {
		return core.RefError("failed to update reflog", err)
	}

	fmt.Printf("Switched to %s '%s'\n", map[bool]string{true: "branch", false: "commit"}[isBranch], target)
	return nil
}

// Legacy function for backward compatibility
func checkout(repo *core.Repository, target string) error {
	return checkoutRepo(repo, target)
}

// updateWorkingDirectoryRepo updates the working directory to match the given tree using Repository context
func updateWorkingDirectoryRepo(repo *core.Repository, tree *objects.TreeObject, basePath string) error {
	currentFiles, err := getWorkingDirFilesRepo(repo)
	if err != nil {
		return fmt.Errorf("failed to scan working directory: %w", err)
	}

	treeFiles := make(map[string]objects.TreeEntry)
	collectTreeEntriesRepo(repo, tree, basePath, treeFiles)

	for relPath, entry := range treeFiles {
		if entry.Type != "blob" {
			continue
		}
		absPath := filepath.Join(repo.Root, relPath)
		blobContent, err := objects.GetBlob(repo.Root, entry.Hash)
		if err != nil {
			return fmt.Errorf("failed to get blob %s: %w", entry.Hash, err)
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", relPath, err)
		}
		if err := os.WriteFile(absPath, blobContent, 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", relPath, err)
		}
		delete(currentFiles, relPath)
	}

	for relPath := range currentFiles {
		absPath := filepath.Join(repo.Root, relPath)
		if err := os.RemoveAll(absPath); err != nil {
			return fmt.Errorf("failed to remove file %s: %w", relPath, err)
		}
	}

	validDirs := make(map[string]struct{})
	collectTreeDirectoriesRepo(repo, tree, basePath, validDirs)
	if err := removeExtraDirectoriesRepo(repo, validDirs); err != nil {
		return fmt.Errorf("failed to remove extra directories: %w", err)
	}

	return nil
}

// Legacy function for backward compatibility
func updateWorkingDirectory(repoRoot string, tree *objects.TreeObject, basePath string) error {
	repo := core.NewRepository(repoRoot)
	return updateWorkingDirectoryRepo(repo, tree, basePath)
}

// getWorkingDirFilesRepo scans the working directory and returns a map of files (excluding .vec directory)
func getWorkingDirFilesRepo(repo *core.Repository) (map[string]struct{}, error) {
	files := make(map[string]struct{})
	err := filepath.Walk(repo.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip .vec directory
		if info.IsDir() && (filepath.Base(path) == ".vec" || strings.HasPrefix(path, filepath.Join(repo.Root, ".vec"))) {
			return filepath.SkipDir
		}
		// Skip directories
		if info.IsDir() {
			return nil
		}
		// Calculate relative path
		relPath, err := filepath.Rel(repo.Root, path)
		if err != nil {
			return err
		}
		files[relPath] = struct{}{}
		return nil
	})
	return files, err
}

// Legacy function for backward compatibility
func getWorkingDirFiles(repoRoot string) (map[string]struct{}, error) {
	repo := core.NewRepository(repoRoot)
	return getWorkingDirFilesRepo(repo)
}

// collectTreeEntriesRepo recursively collects all blob entries from a tree and subtrees
func collectTreeEntriesRepo(repo *core.Repository, tree *objects.TreeObject, prefix string, entries map[string]objects.TreeEntry) {
	for _, entry := range tree.Entries {
		entryPath := filepath.Join(prefix, entry.Name)
		entries[entryPath] = entry

		if entry.Type == "tree" {
			subTree, err := objects.GetTree(repo.Root, entry.Hash)
			if err == nil {
				collectTreeEntriesRepo(repo, subTree, entryPath, entries)
			}
		}
	}
}

// Legacy function for backward compatibility
func collectTreeEntries(repoRoot string, tree *objects.TreeObject, prefix string, entries map[string]objects.TreeEntry) {
	repo := core.NewRepository(repoRoot)
	collectTreeEntriesRepo(repo, tree, prefix, entries)
}

// collectTreeDirectoriesRepo recursively collects all directory paths from a tree and subtrees
func collectTreeDirectoriesRepo(repo *core.Repository, tree *objects.TreeObject, prefix string, dirs map[string]struct{}) {
	// Add current directory
	if prefix != "" {
		dirs[prefix] = struct{}{}
	}

	for _, entry := range tree.Entries {
		if entry.Type == "tree" {
			entryPath := filepath.Join(prefix, entry.Name)
			dirs[entryPath] = struct{}{}

			subTree, err := objects.GetTree(repo.Root, entry.Hash)
			if err == nil {
				collectTreeDirectoriesRepo(repo, subTree, entryPath, dirs)
			}
		}
	}
}

// Legacy function for backward compatibility
func collectTreeDirectories(repoRoot string, tree *objects.TreeObject, prefix string, dirs map[string]struct{}) {
	repo := core.NewRepository(repoRoot)
	collectTreeDirectoriesRepo(repo, tree, prefix, dirs)
}

// removeExtraDirectoriesRepo removes directories that aren't in the validDirs map
func removeExtraDirectoriesRepo(repo *core.Repository, validDirs map[string]struct{}) error {
	// Get all directories (excluding .vec)
	var dirs []string
	err := filepath.Walk(repo.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		if filepath.Base(path) == ".vec" || strings.HasPrefix(path, filepath.Join(repo.Root, ".vec")) {
			return filepath.SkipDir
		}
		if path == repo.Root {
			return nil // Skip root directory
		}
		relPath, err := filepath.Rel(repo.Root, path)
		if err != nil {
			return err
		}
		dirs = append(dirs, relPath)
		return nil
	})
	if err != nil {
		return err
	}

	// Sort directories by depth (deepest first) to ensure we don't remove a parent before a child
	sort.Slice(dirs, func(i, j int) bool {
		return len(strings.Split(dirs[i], string(filepath.Separator))) > len(strings.Split(dirs[j], string(filepath.Separator)))
	})

	// Remove directories not in validDirs
	for _, dir := range dirs {
		if _, valid := validDirs[dir]; !valid {
			dirPath := filepath.Join(repo.Root, dir)
			// Check if the directory is empty (only remove empty directories)
			entries, err := os.ReadDir(dirPath)
			if err == nil && len(entries) == 0 {
				if err := os.Remove(dirPath); err != nil {
					return fmt.Errorf("failed to remove directory %s: %w", dir, err)
				}
			}
		}
	}
	return nil
}

// Legacy function for backward compatibility
func removeExtraDirectories(repoRoot string, validDirs map[string]struct{}) error {
	repo := core.NewRepository(repoRoot)
	return removeExtraDirectoriesRepo(repo, validDirs)
}

// createIndexFromTreeRepo creates a new index from a tree using Repository context
func createIndexFromTreeRepo(repo *core.Repository, tree *objects.TreeObject, basePath string) (*staging.Index, error) {
	index := staging.NewIndexRepo(repo)

	// Collect all blob entries
	entries := make(map[string]objects.TreeEntry)
	collectTreeEntriesRepo(repo, tree, basePath, entries)

	// Add each entry to the index
	for path, entry := range entries {
		if entry.Type == "blob" {
			absPath := filepath.Join(repo.Root, path)
			// If the file doesn't exist yet, we'll create it
			if !utils.FileExists(absPath) {
				blobContent, err := objects.GetBlob(repo.Root, entry.Hash)
				if err != nil {
					return nil, fmt.Errorf("failed to get blob %s: %w", entry.Hash, err)
				}
				dir := filepath.Dir(absPath)
				if err := os.MkdirAll(dir, 0755); err != nil {
					return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
				}
				if err := os.WriteFile(absPath, blobContent, 0644); err != nil {
					return nil, fmt.Errorf("failed to write file %s: %w", path, err)
				}
			}

			if err := index.AddRepo(repo, path, entry.Hash); err != nil {
				return nil, fmt.Errorf("failed to add %s to index: %w", path, err)
			}
		}
	}

	return index, nil
}

// Legacy function for backward compatibility
func createIndexFromTree(repoRoot string, tree *objects.TreeObject, basePath string) (*staging.Index, error) {
	repo := core.NewRepository(repoRoot)
	return createIndexFromTreeRepo(repo, tree, basePath)
}

// updateReflogForCheckoutRepo updates the reflog for the given reference using Repository context
func updateReflogForCheckoutRepo(repo *core.Repository, prevCommitID, newCommitID, ref, action, details string) error {
	reflogDir := filepath.Join(repo.VecDir, "logs", "refs", "heads")
	headReflogPath := filepath.Join(repo.VecDir, "logs", "HEAD")

	// Ensure reflog directories exist
	if err := os.MkdirAll(reflogDir, 0755); err != nil {
		return fmt.Errorf("failed to create reflog directory: %w", err)
	}

	// Create HEAD reflog path directory
	if err := os.MkdirAll(filepath.Dir(headReflogPath), 0755); err != nil {
		return fmt.Errorf("failed to create HEAD reflog directory: %w", err)
	}

	// Format reflog entry
	now := time.Now()
	username := os.Getenv("USER")
	if username == "" {
		username = "unknown"
	}

	// Get hostname or use a default
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}
	email := username + "@" + hostname

	entry := fmt.Sprintf("%s %s %s <%s> %d +0000\t%s: %s\n",
		prevCommitID, newCommitID, username, email,
		now.Unix(), action, details)

	// Update HEAD reflog
	if utils.FileExists(headReflogPath) {
		// Append to existing HEAD reflog
		f, err := os.OpenFile(headReflogPath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open HEAD reflog: %w", err)
		}
		defer f.Close()
		if _, err := f.WriteString(entry); err != nil {
			return fmt.Errorf("failed to write to HEAD reflog: %w", err)
		}
	} else {
		// Create new HEAD reflog
		if err := os.WriteFile(headReflogPath, []byte(entry), 0644); err != nil {
			return fmt.Errorf("failed to create HEAD reflog: %w", err)
		}
	}

	// Also update branch reflog if this is a branch operation
	if ref != "(HEAD detached)" {
		branchReflogPath := filepath.Join(reflogDir, ref)

		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(branchReflogPath), 0755); err != nil {
			return fmt.Errorf("failed to create branch reflog directory: %w", err)
		}

		if utils.FileExists(branchReflogPath) {
			// Append to existing branch reflog
			f, err := os.OpenFile(branchReflogPath, os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("failed to open branch reflog: %w", err)
			}
			defer f.Close()
			if _, err := io.WriteString(f, entry); err != nil {
				return fmt.Errorf("failed to write to branch reflog: %w", err)
			}
		} else {
			// Create new branch reflog
			if err := os.WriteFile(branchReflogPath, []byte(entry), 0644); err != nil {
				return fmt.Errorf("failed to create branch reflog: %w", err)
			}
		}
	}

	return nil
}

// Legacy function for backward compatibility
func updateReflog(repoRoot, prevCommitID, newCommitID, ref, action, details string) error {
	repo := core.NewRepository(repoRoot)
	return updateReflogForCheckoutRepo(repo, prevCommitID, newCommitID, ref, action, details)
}

func init() {
	checkoutCmd := NewRepoCommand(
		"checkout <branch-or-commit>",
		"Switch branches or restore working tree files",
		CheckoutHandler,
	)

	checkoutCmd.Long = `Switch branches or restore working tree files.
For branch operations, this updates the index and working tree to match
the branch, and points HEAD at the branch head.

Examples:
  vec checkout main           # Switch to branch 'main'
  vec checkout -b feature     # Create and switch to branch 'feature'
  vec checkout e12f109        # Detach HEAD at commit e12f109
  vec checkout --force main   # Discard local changes and checkout 'main'`

	checkoutCmd.Args = cobra.ExactArgs(1)

	checkoutCmd.Flags().BoolVarP(&createBranch, "create-branch", "b", false, "Create a new branch at the target and switch to it")
	checkoutCmd.Flags().BoolVarP(&forceCheckout, "force", "f", false, "Force checkout (discard local changes)")

	// For consistency with git, add a -B alias that does the same as -b
	checkoutCmd.Flags().BoolVarP(&createBranch, "create", "B", false, "Create a new branch at the target and switch to it")

	rootCmd.AddCommand(checkoutCmd)
}

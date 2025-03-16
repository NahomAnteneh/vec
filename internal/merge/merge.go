package merge

import (
	"bufio"
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

// MergeStrategy represents an auto-resolution strategy.
type MergeStrategy string

const (
	MergeStrategyRecursive MergeStrategy = "recursive" // default three-way merge
	MergeStrategyOurs      MergeStrategy = "ours"      // always use our changes
	MergeStrategyTheirs    MergeStrategy = "theirs"    // always use their changes
)

// MergeConfig holds options to influence merge behavior.
type MergeConfig struct {
	Strategy    MergeStrategy // Strategy for conflict resolution
	Interactive bool          // Whether to prompt user interactively on conflicts
}

// MergeResult captures the outcome of the merge operation.
type MergeResult struct {
	HasConflicts bool
}

// Merge performs a merge of the sourceBranch into the current branch in the repository at repoRoot.
// It accepts a configuration for advanced merging behaviors.
func Merge(repoRoot, sourceBranch string, config *MergeConfig) (bool, error) {
	if config == nil {
		// Default to recursive (normal three-way merge with conflict markers) and non-interactive.
		config = &MergeConfig{Strategy: MergeStrategyRecursive, Interactive: false}
	}

	// Validate repository and load index.
	vecDir := filepath.Join(repoRoot, ".vec")
	if _, err := os.Stat(vecDir); os.IsNotExist(err) {
		return false, fmt.Errorf("not a vec repository: %s", repoRoot)
	}
	index, err := staging.LoadIndex(repoRoot)
	if err != nil {
		return false, fmt.Errorf("failed to load index: %w", err)
	}

	// Check for uncommitted changes.
	if index.HasUncommittedChanges(repoRoot) {
		return false, fmt.Errorf("uncommitted changes detected; commit or stash them before merging")
	}

	// Load current branch and HEAD.
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

	// Load source branch commit.
	sourceBranchFile := filepath.Join(vecDir, "refs", "heads", sourceBranch)
	sourceCommitIDBytes, err := os.ReadFile(sourceBranchFile)
	if err != nil {
		return false, fmt.Errorf("failed to read source branch '%s': %w", sourceBranch, err)
	}
	sourceCommitID := strings.TrimSpace(string(sourceCommitIDBytes))

	// Prevent self-merge.
	if currentBranch == sourceBranch {
		return false, fmt.Errorf("cannot merge a branch with itself")
	}

	// Find merge base.
	baseCommitID, err := findMergeBase(repoRoot, headCommitID, sourceCommitID)
	if err != nil {
		return false, fmt.Errorf("failed to find merge base: %w", err)
	}

	// Handle fast-forward or already up-to-date cases.
	if baseCommitID == headCommitID {
		// Fast-forward: current branch is behind source branch.
		if err := CheckoutCommit(repoRoot, sourceCommitID); err != nil {
			return false, fmt.Errorf("failed to checkout source commit for fast-forward: %w", err)
		}
		branchFile := filepath.Join(vecDir, "refs", "heads", currentBranch)
		if err := os.WriteFile(branchFile, []byte(sourceCommitID), 0644); err != nil {
			return false, fmt.Errorf("failed to update branch pointer: %w", err)
		}
		fmt.Println("Fast-forward merge completed.")
		return false, nil
	} else if baseCommitID == sourceCommitID {
		// Already up-to-date.
		return false, fmt.Errorf("already up-to-date")
	}

	// Load commit objects.
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

	// Load tree objects.
	baseTree, err := objects.GetTree(baseCommit.Tree)
	if err != nil {
		return false, fmt.Errorf("failed to load base tree: %w", err)
	}
	ourTree, err := objects.GetTree(ourCommit.Tree)
	if err != nil {
		return false, fmt.Errorf("failed to load our tree: %w", err)
	}
	theirTree, err := objects.GetTree(theirCommit.Tree)
	if err != nil {
		return false, fmt.Errorf("failed to load their tree: %w", err)
	}

	// Perform the three-way merge.
	result, err := performMerge(repoRoot, index, baseTree, ourTree, theirTree, config)
	if err != nil {
		return false, fmt.Errorf("merge failed: %w", err)
	}

	// Write updated index.
	if err := index.Write(); err != nil {
		return false, fmt.Errorf("failed to write index: %w", err)
	}

	if result.HasConflicts {
		fmt.Println("Merge conflicts detected. Please resolve them and commit the result.")
		return true, nil
	}

	// Create tree from merged index.
	treeID, err := staging.CreateTreeFromIndex(repoRoot, index)
	if err != nil {
		return false, fmt.Errorf("failed to create tree from index: %w", err)
	}

	// Create merge commit.
	author := ourCommit.Author
	committer := ourCommit.Committer
	if committer == "" {
		committer = author
	}
	message := fmt.Sprintf("Merge branch '%s' into %s", sourceBranch, currentBranch)
	timestamp := time.Now().Unix()
	commitHash, err := objects.CreateCommit(repoRoot, treeID, []string{headCommitID, sourceCommitID}, author, committer, message, timestamp)
	if err != nil {
		return false, fmt.Errorf("failed to create merge commit: %w", err)
	}

	// Update branch pointer.
	branchFile := filepath.Join(vecDir, "refs", "heads", currentBranch)
	if err := os.WriteFile(branchFile, []byte(commitHash), 0644); err != nil {
		return false, fmt.Errorf("failed to update branch pointer: %w", err)
	}

	fmt.Println("Merge completed successfully.")
	return false, nil
}

// performMerge executes a three-way merge between base, ours, and theirs trees, updating the index and working directory.
func performMerge(repoRoot string, index *staging.Index, baseTree, ourTree, theirTree *objects.TreeObject, config *MergeConfig) (MergeResult, error) {
	var result MergeResult

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

		switch {
		case baseExists && ourExists && theirExists:
			// Unchanged in both?
			if baseEntry.Hash == ourEntry.Hash && baseEntry.Hash == theirEntry.Hash {
				continue
			} else if baseEntry.Hash == ourEntry.Hash {
				// Modified only in theirs.
				if err := copyBlobAndAddToIndex(repoRoot, index, theirEntry.Hash, filePath, theirEntry.Mode); err != nil {
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
				if err := resolveConflict(repoRoot, index, filePath,
					baseEntry.Hash, ourEntry.Hash, theirEntry.Hash,
					baseEntry.Mode, ourEntry.Mode, theirEntry.Mode,
					config); err != nil {
					return MergeResult{}, err
				}
				result.HasConflicts = true
			}

		case baseExists && ourExists && !theirExists:
			// File deleted in theirs.
			if ourEntry.Hash == baseEntry.Hash {
				if err := os.Remove(filepath.Join(repoRoot, filePath)); err != nil && !os.IsNotExist(err) {
					return MergeResult{}, fmt.Errorf("failed to remove file '%s': %w", filePath, err)
				}
				if err := index.Remove(repoRoot, filePath); err != nil {
					return MergeResult{}, err
				}
			} else {
				if err := resolveConflict(repoRoot, index, filePath,
					baseEntry.Hash, ourEntry.Hash, "",
					baseEntry.Mode, ourEntry.Mode, 0,
					config); err != nil {
					return MergeResult{}, err
				}
				result.HasConflicts = true
			}

		case baseExists && !ourExists && theirExists:
			// File deleted in ours.
			if theirEntry.Hash == baseEntry.Hash {
				if err := os.Remove(filepath.Join(repoRoot, filePath)); err != nil && !os.IsNotExist(err) {
					return MergeResult{}, fmt.Errorf("failed to remove file '%s': %w", filePath, err)
				}
				if err := index.Remove(repoRoot, filePath); err != nil {
					return MergeResult{}, err
				}
			} else {
				if err := resolveConflict(repoRoot, index, filePath,
					baseEntry.Hash, "", theirEntry.Hash,
					baseEntry.Mode, 0, theirEntry.Mode,
					config); err != nil {
					return MergeResult{}, err
				}
				result.HasConflicts = true
			}

		case !baseExists && ourExists && theirExists:
			// File added in both branches.
			if ourEntry.Hash == theirEntry.Hash {
				continue
			} else {
				if err := resolveConflict(repoRoot, index, filePath,
					"", ourEntry.Hash, theirEntry.Hash,
					0, ourEntry.Mode, theirEntry.Mode,
					config); err != nil {
					return MergeResult{}, err
				}
				result.HasConflicts = true
			}

		case !baseExists && ourExists && !theirExists:
			// File added only in ours.
			continue

		case !baseExists && !ourExists && theirExists:
			// File added only in theirs.
			if err := copyBlobAndAddToIndex(repoRoot, index, theirEntry.Hash, filePath, theirEntry.Mode); err != nil {
				return MergeResult{}, err
			}

		case baseExists && !ourExists && !theirExists:
			// File deleted in both branches.
			continue
		}
	}

	return result, nil
}

// resolveConflict applies advanced conflict resolution based on configuration.
func resolveConflict(repoRoot string, index *staging.Index, filePath, baseHash, ourHash, theirHash string, baseMode, ourMode, theirMode int32, config *MergeConfig) error {
	// If an auto-resolution strategy is selected (ours/theirs), use it.
	switch config.Strategy {
	case MergeStrategyOurs:
		if ourHash != "" {
			return copyBlobAndAddToIndex(repoRoot, index, ourHash, filePath, ourMode)
		}
		return fmt.Errorf("missing 'ours' version for %s", filePath)
	case MergeStrategyTheirs:
		if theirHash != "" {
			return copyBlobAndAddToIndex(repoRoot, index, theirHash, filePath, theirMode)
		}
		return fmt.Errorf("missing 'theirs' version for %s", filePath)
		// For recursive, fall through for interactive/manual merge.
	}

	// Default (recursive) strategy with interactive prompt if enabled.
	if config.Interactive && isTerminal(os.Stdin.Fd()) {
		resolvedContent, err := interactiveConflictPrompt(filePath, baseHash, ourHash, theirHash, repoRoot)
		if err == nil && len(resolvedContent) > 0 {
			absPath := filepath.Join(repoRoot, filePath)
			mode := ourMode
			if mode == 0 {
				mode = theirMode
			}
			if mode == 0 {
				mode = baseMode
			}
			if err := os.WriteFile(absPath, resolvedContent, os.FileMode(mode)); err != nil {
				return fmt.Errorf("failed to write file after interactive merge of '%s': %w", filePath, err)
			}
			// Update index with the resolved blob.
			blobHash, err := objects.CreateBlob(repoRoot, resolvedContent)
			if err != nil {
				return fmt.Errorf("failed to write blob for '%s': %w", filePath, err)
			}
			if err := index.Add(repoRoot, filePath, blobHash); err != nil {
				return fmt.Errorf("failed to update index for '%s': %w", filePath, err)
			}
			return nil
		}
		// If interactive resolution fails, fall back to conflict markers.
	}

	// Fallback: write file with conflict markers.
	return writeConflictFile(repoRoot, index, filePath, baseHash, ourHash, theirHash, baseMode, ourMode, theirMode)
}

// writeConflictFile constructs a file with conflict markers and updates the index.
func writeConflictFile(repoRoot string, index *staging.Index, filePath, baseHash, ourHash, theirHash string, baseMode, ourMode, theirMode int32) error {
	var baseContent, ourContent, theirContent []byte
	var err error
	if baseHash != "" {
		baseContent, err = objects.GetBlob(repoRoot, baseHash)
		if err != nil {
			return fmt.Errorf("failed to get base blob '%s': %w", baseHash, err)
		}
	}
	if ourHash != "" {
		ourContent, err = objects.GetBlob(repoRoot, ourHash)
		if err != nil {
			return fmt.Errorf("failed to get our blob '%s': %w", ourHash, err)
		}
	}
	if theirHash != "" {
		theirContent, err = objects.GetBlob(repoRoot, theirHash)
		if err != nil {
			return fmt.Errorf("failed to get their blob '%s': %w", theirHash, err)
		}
	}

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

	absPath := filepath.Join(repoRoot, filePath)
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

	if err := os.WriteFile(absPath, conflictContent.Bytes(), os.FileMode(mode)); err != nil {
		return fmt.Errorf("failed to write conflict file '%s': %w", filePath, err)
	}

	// Update index conflict entries.
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

// interactiveConflictPrompt prompts the user for how to resolve a conflict on filePath.
// It returns the resolved file content.
func interactiveConflictPrompt(filePath, baseHash, ourHash, theirHash, repoRoot string) ([]byte, error) {
	var baseContent, ourContent, theirContent []byte
	var err error
	if baseHash != "" {
		baseContent, err = objects.GetBlob(repoRoot, baseHash)
		if err != nil {
			return nil, fmt.Errorf("failed to get base blob '%s': %w", baseHash, err)
		}
	}
	if ourHash != "" {
		ourContent, err = objects.GetBlob(repoRoot, ourHash)
		if err != nil {
			return nil, fmt.Errorf("failed to get our blob '%s': %w", ourHash, err)
		}
	}
	if theirHash != "" {
		theirContent, err = objects.GetBlob(repoRoot, theirHash)
		if err != nil {
			return nil, fmt.Errorf("failed to get their blob '%s': %w", theirHash, err)
		}
	}

	fmt.Printf("Conflict detected in '%s'.\n", filePath)
	fmt.Println("Select resolution option:")
	fmt.Println("[1] Use ours")
	fmt.Println("[2] Use theirs")
	fmt.Println("[3] Use both with conflict markers (default)")
	fmt.Print("Enter choice (1/2/3): ")

	reader := bufio.NewReader(os.Stdin)
	choice, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read user input: %w", err)
	}
	choice = strings.TrimSpace(choice)
	switch choice {
	case "1":
		return ourContent, nil
	case "2":
		return theirContent, nil
	default:
		// Use conflict markers.
		var buf bytes.Buffer
		buf.WriteString("<<<<<<< ours\n")
		buf.Write(ourContent)
		buf.WriteString("\n||||||| base\n")
		buf.Write(baseContent)
		buf.WriteString("\n=======\n")
		buf.Write(theirContent)
		buf.WriteString("\n>>>>>>> theirs")
		return buf.Bytes(), nil
	}
}

// copyBlobAndAddToIndex copies a blob to the working directory and adds it to the index.
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

// Updated findMergeBase to fix history checks
func findMergeBase(repoRoot, commit1, commit2 string) (string, error) {
	// If the commits are the same, that's the merge base.
	if commit1 == commit2 {
		return commit1, nil
	}

	// Collect all ancestors of commit1.
	ancestors := make(map[string]bool)
	var collect func(string) error
	collect = func(commitID string) error {
		if ancestors[commitID] {
			return nil
		}
		ancestors[commitID] = true
		commit, err := objects.GetCommit(repoRoot, commitID)
		if err != nil {
			return fmt.Errorf("failed to load commit %s: %w", commitID, err)
		}
		for _, parent := range commit.Parents {
			if err := collect(parent); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collect(commit1); err != nil {
		return "", fmt.Errorf("failed to collect ancestors for commit1: %w", err)
	}

	// Traverse commit2 ancestry (breadth-first) and return the first shared commit.
	queue := []string{commit2}
	visited := make(map[string]bool)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if ancestors[current] {
			return current, nil
		}
		if visited[current] {
			continue
		}
		visited[current] = true
		commit, err := objects.GetCommit(repoRoot, current)
		if err != nil {
			return "", fmt.Errorf("failed to load commit %s: %w", current, err)
		}
		for _, parent := range commit.Parents {
			queue = append(queue, parent)
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
	tree, err := objects.GetTree(commit.Tree)
	if err != nil {
		return fmt.Errorf("failed to load tree %s: %w", commit.Tree, err)
	}
	if err := updateWorkingDirectory(repoRoot, tree, ""); err != nil {
		return fmt.Errorf("failed to update working directory: %w", err)
	}
	index, err := createIndexFromTree(repoRoot, tree, "")
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
			if err := os.WriteFile(absPath, content, os.FileMode(entry.Mode)); err != nil {
				return fmt.Errorf("failed to write file '%s': %w", currentPath, err)
			}
		} else if entry.Type == "tree" {
			subTree, err := objects.GetTree(entry.Hash)
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
			subTree, err := objects.GetTree(entry.Hash)
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

// isTerminal returns true when fd is a terminal.
func isTerminal(fd uintptr) bool {
	// Production-ready check. You might use a library such as "golang.org/x/term".
	return true
}

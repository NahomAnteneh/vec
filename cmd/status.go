package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var (
	statusShort  bool
	statusBranch bool
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

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().BoolVarP(&statusShort, "short", "s", false, "Give the output in the short-format")
	statusCmd.Flags().BoolVarP(&statusBranch, "branch", "b", false, "Show branch information even in short-format")
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
	statusInfo, err := compareStatus(repoRoot, index, commitTree)
	if err != nil {
		return fmt.Errorf("failed to compare status: %w", err)
	}

	// Get current branch
	branchName, err := utils.GetCurrentBranch(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	// Print status in the requested format
	if statusShort {
		printShortStatus(branchName, statusInfo)
	} else {
		printLongStatus(branchName, statusInfo, index)
	}

	return nil
}

// StatusInfo holds all the information about the working tree status
type StatusInfo struct {
	NewFiles          []string
	StagedModified    []string
	StagedDeleted     []string
	Untracked         []string
	ModifiedNotStaged []string
	DeletedNotStaged  []string
	Conflicts         []string
	IsClean           bool
}

// printLongStatus outputs the status in the standard long format
func printLongStatus(branchName string, info *StatusInfo, index *staging.Index) {
	fmt.Printf("On branch %s\n", branchName)

	// Check for merge conflicts
	if len(info.Conflicts) > 0 {
		fmt.Println("\nYou have unmerged paths.")
		fmt.Println("  (fix conflicts and run \"vec commit\")")
		fmt.Println("  (use \"vec merge --abort\" to abort the merge)")
		fmt.Println()
		fmt.Println("Unmerged paths:")
		for _, file := range info.Conflicts {
			fmt.Printf("\tboth modified:   %s\n", file)
		}
		fmt.Println()
	}

	// Output "Changes to be committed"
	if len(info.NewFiles) > 0 || len(info.StagedModified) > 0 || len(info.StagedDeleted) > 0 {
		fmt.Println("Changes to be committed:")
		fmt.Println("  (use \"vec restore --staged <file>...\" to unstage)")
		fmt.Println()

		sort.Strings(info.NewFiles)
		for _, file := range info.NewFiles {
			fmt.Printf("\tnew file:   %s\n", file)
		}

		sort.Strings(info.StagedModified)
		for _, file := range info.StagedModified {
			fmt.Printf("\tmodified:   %s\n", file)
		}

		sort.Strings(info.StagedDeleted)
		for _, file := range info.StagedDeleted {
			fmt.Printf("\tdeleted:    %s\n", file)
		}
		fmt.Println()
	}

	// Output "Changes not staged for commit"
	if len(info.ModifiedNotStaged) > 0 || len(info.DeletedNotStaged) > 0 {
		fmt.Println("Changes not staged for commit:")
		fmt.Println("  (use \"vec add <file>...\" to update what will be committed)")
		fmt.Println("  (use \"vec restore <file>...\" to discard changes in working directory)")
		fmt.Println()

		sort.Strings(info.ModifiedNotStaged)
		for _, file := range info.ModifiedNotStaged {
			fmt.Printf("\tmodified:   %s\n", file)
		}

		sort.Strings(info.DeletedNotStaged)
		for _, file := range info.DeletedNotStaged {
			fmt.Printf("\tdeleted:    %s\n", file)
		}
		fmt.Println()
	}

	// Output "Untracked files"
	if len(info.Untracked) > 0 {
		fmt.Println("Untracked files:")
		fmt.Println("  (use \"vec add <file>...\" to include in what will be committed)")
		fmt.Println()

		sort.Strings(info.Untracked)
		for _, file := range info.Untracked {
			fmt.Printf("\t%s\n", file)
		}
		fmt.Println()
	}

	// Output "nothing to commit" if working tree is clean
	if info.IsClean {
		fmt.Println("nothing to commit, working tree clean")
	}
}

// printShortStatus outputs the status in the short format (similar to git status -s)
func printShortStatus(branchName string, info *StatusInfo) {
	if statusBranch {
		fmt.Printf("## %s\n", branchName)
	}

	// Map of all files to their status codes
	fileStatuses := make(map[string]string)

	// Process all categories and assign their status codes
	for _, file := range info.StagedDeleted {
		fileStatuses[file] = "D "
	}

	for _, file := range info.NewFiles {
		fileStatuses[file] = "A "
	}

	for _, file := range info.StagedModified {
		fileStatuses[file] = "M "
	}

	// Process working tree changes
	for _, file := range info.DeletedNotStaged {
		if status, exists := fileStatuses[file]; exists {
			fileStatuses[file] = status[:1] + "D"
		} else {
			fileStatuses[file] = " D"
		}
	}

	for _, file := range info.ModifiedNotStaged {
		if status, exists := fileStatuses[file]; exists {
			fileStatuses[file] = status[:1] + "M"
		} else {
			fileStatuses[file] = " M"
		}
	}

	// Add untracked files
	for _, file := range info.Untracked {
		fileStatuses[file] = "??"
	}

	// Add conflicts
	for _, file := range info.Conflicts {
		fileStatuses[file] = "UU"
	}

	// Get all files and sort them
	var files []string
	for file := range fileStatuses {
		files = append(files, file)
	}
	sort.Strings(files)

	// Print in short format
	for _, file := range files {
		status := fileStatuses[file]
		fmt.Printf("%s %s\n", status, file)
	}
}

// computeFileBlobHash computes the SHA256 hash for a file
// exactly as objects.CreateBlob does by including the header "blob <size>\0".
func computeFileBlobHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return utils.HashBytes("blob", data), nil
}

// compareStatus compares the commit tree, index, and working directory.
// Returns a StatusInfo struct with all the status information.
func compareStatus(repoRoot string, index *staging.Index, commitTree *objects.TreeObject) (*StatusInfo, error) {
	// Initialize result with capacity estimates
	result := &StatusInfo{
		NewFiles:          make([]string, 0, 10),
		StagedModified:    make([]string, 0, 10),
		StagedDeleted:     make([]string, 0, 10),
		Untracked:         make([]string, 0, 10),
		ModifiedNotStaged: make([]string, 0, 10),
		DeletedNotStaged:  make([]string, 0, 10),
		Conflicts:         make([]string, 0, 5),
		IsClean:           true,
	}

	// Check for conflicts first
	if index.HasConflicts() {
		conflicts := index.GetConflicts()
		for file := range conflicts {
			result.Conflicts = append(result.Conflicts, file)
			result.IsClean = false
		}
		sort.Strings(result.Conflicts)
	}

	// Build maps for efficient lookup
	indexMap := make(map[string]string, len(index.Entries))  // path -> SHA256
	indexStages := make(map[string]bool, len(index.Entries)) // paths that have any stage entry

	for _, entry := range index.Entries {
		if entry.Stage == 0 {
			indexMap[entry.FilePath] = entry.SHA256
		}
		indexStages[entry.FilePath] = true
	}

	// Build commit tree map
	commitTreeMap := make(map[string]objects.TreeEntry, 100) // path -> TreeEntry
	buildCommitTreeMap(repoRoot, commitTree, "", commitTreeMap)

	// Track files seen in the working directory to detect deletions
	workingDirFiles := make(map[string]bool, 100)

	// Use a sync.Map for thread-safe concurrent access
	var fileHashes sync.Map

	// Pre-compute file hashes for files we know we'll need
	var hashWg sync.WaitGroup
	filesToHash := make(chan string, 100)

	// Start workers to hash files concurrently
	const numWorkers = 4
	for i := 0; i < numWorkers; i++ {
		hashWg.Add(1)
		go func() {
			defer hashWg.Done()
			for absPath := range filesToHash {
				if hash, err := computeFileBlobHash(absPath); err == nil {
					fileHashes.Store(absPath, hash)
				}
			}
		}()
	}

	// Walk the working directory to find all files
	err := filepath.Walk(repoRoot, func(absPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip .vec directory
		if strings.Contains(absPath, string(filepath.Separator)+".vec"+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip ignored files and directories
		if isIgnored, _ := utils.IsIgnored(repoRoot, absPath); isIgnored {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(repoRoot, absPath)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Use normalized path throughout
		relPath = filepath.ToSlash(relPath)
		workingDirFiles[relPath] = true

		// Queue this file for hashing
		filesToHash <- absPath
		return nil
	})

	// Close the channel after all files are queued
	close(filesToHash)

	// Wait for all hashing to complete
	hashWg.Wait()

	if err != nil {
		return nil, fmt.Errorf("failed to walk working directory: %w", err)
	}

	// Process working directory files
	for relPath := range workingDirFiles {
		absPath := filepath.Join(repoRoot, relPath)

		// Get file hash
		hashVal, ok := fileHashes.Load(absPath)
		if !ok {
			continue // Skip if we couldn't get the hash
		}
		currentHash := hashVal.(string)

		indexHash, inIndex := indexMap[relPath]
		commitEntry, inCommit := commitTreeMap[relPath]

		// Check for conflicts first
		if _, isConflict := index.GetEntry(relPath, 1); isConflict {
			// Already handled in the conflict check above
			continue
		}

		// Case 1: File in commit, index, and working directory
		if inCommit && inIndex {
			if indexHash != commitEntry.Hash {
				result.StagedModified = append(result.StagedModified, relPath)
				result.IsClean = false
			}

			if currentHash != indexHash {
				result.ModifiedNotStaged = append(result.ModifiedNotStaged, relPath)
				result.IsClean = false
			}
		} else if inIndex && !inCommit {
			// Case 2: File in index but not in commit (new file)
			result.NewFiles = append(result.NewFiles, relPath)
			result.IsClean = false

			if currentHash != indexHash {
				result.ModifiedNotStaged = append(result.ModifiedNotStaged, relPath)
			}
		} else if !inIndex && !inCommit {
			// Case 3: File not in index or commit (untracked)
			result.Untracked = append(result.Untracked, relPath)
			result.IsClean = false
		}
	}

	// Check for deleted files
	// Files in commit but not in working directory or index
	for relPath := range commitTreeMap {
		_, inIndex := indexMap[relPath]
		inWorkingDir := workingDirFiles[relPath]

		if !inIndex && !inWorkingDir {
			result.StagedDeleted = append(result.StagedDeleted, relPath)
			result.IsClean = false
		} else if inIndex && !inWorkingDir {
			result.DeletedNotStaged = append(result.DeletedNotStaged, relPath)
			result.IsClean = false
		}
	}

	// Sort all arrays for consistent output
	sort.Strings(result.NewFiles)
	sort.Strings(result.StagedModified)
	sort.Strings(result.StagedDeleted)
	sort.Strings(result.Untracked)
	sort.Strings(result.ModifiedNotStaged)
	sort.Strings(result.DeletedNotStaged)

	return result, nil
}

// buildCommitTreeMap recursively builds a map of commit tree entries.
func buildCommitTreeMap(repoRoot string, tree *objects.TreeObject, parentPath string, treeMap map[string]objects.TreeEntry) {
	if tree == nil {
		return
	}

	for _, entry := range tree.Entries {
		entryPath := filepath.Join(parentPath, entry.Name)
		// Normalize path separators
		entryPath = strings.ReplaceAll(entryPath, string(filepath.Separator), "/")

		if entry.Type == "blob" {
			treeMap[entryPath] = entry
		} else if entry.Type == "tree" {
			subTree, err := objects.GetTree(repoRoot, entry.Hash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Error getting subtree %s: %v\n", entry.Hash, err)
				continue
			}
			buildCommitTreeMap(repoRoot, subTree, entryPath, treeMap)
		}
	}
}

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/NahomAnteneh/vec/utils"
)

var (
	statusShort  bool
	statusBranch bool
)

// StatusHandler handles the 'status' command
func StatusHandler(repo *core.Repository, args []string) error {
	// Load the index
	index, err := staging.LoadIndex(repo)
	if err != nil {
		return fmt.Errorf("failed to read index: %w", err)
	}

	// Get the HEAD commit and its tree
	headCommitID, err := repo.ReadHead()
	if err != nil {
		return fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	var commitTree *objects.TreeObject
	if headCommitID != "" {
		headCommit, err := objects.GetCommit(repo.Root, headCommitID)
		if err != nil {
			return fmt.Errorf("failed to load HEAD commit: %w", err)
		}
		commitTree, err = objects.GetTree(repo.Root, headCommit.Tree)
		if err != nil {
			return fmt.Errorf("failed to load commit tree: %w", err)
		}
	} else {
		commitTree = objects.NewTreeObject() // Empty tree for new repo
	}

	// Compare states
	statusInfo, err := compareStatus(repo, index, commitTree)
	if err != nil {
		return fmt.Errorf("failed to compare status: %w", err)
	}

	// Get current branch
	branchName, err := repo.GetCurrentBranch()
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

func init() {
	statusCmd := NewRepoCommand(
		"status",
		"Show the working tree status",
		StatusHandler,
	)
	statusCmd.Flags().BoolVarP(&statusShort, "short", "s", false, "Give the output in the short-format")
	statusCmd.Flags().BoolVarP(&statusBranch, "branch", "b", false, "Show branch information even in short-format")
	rootCmd.AddCommand(statusCmd)
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

// compareStatus compares the working directory, index, and commit tree
// to determine the status of files in the repository using Repository context
func compareStatus(repo *core.Repository, index *staging.Index, commitTree *objects.TreeObject) (*StatusInfo, error) {
	status := &StatusInfo{
		NewFiles:          []string{},
		StagedModified:    []string{},
		StagedDeleted:     []string{},
		Untracked:         []string{},
		ModifiedNotStaged: []string{},
		DeletedNotStaged:  []string{},
		Conflicts:         []string{},
		IsClean:           true,
	}

	// Check for conflicts first
	for _, entry := range index.Entries {
		if entry.Stage > 0 {
			// This is a conflict entry; add if not already in the list
			found := false
			for _, path := range status.Conflicts {
				if path == entry.FilePath {
					found = true
					break
				}
			}
			if !found {
				status.Conflicts = append(status.Conflicts, entry.FilePath)
				status.IsClean = false
			}
		}
	}

	// Build a map from file path to tree entry from the commit tree
	commitTreeMap := make(map[string]objects.TreeEntry)
	if commitTree != nil {
		buildCommitTreeMap(repo, commitTree, "", commitTreeMap)
	}

	// Build a map of staged files
	stagedFiles := make(map[string]staging.IndexEntry)
	for _, entry := range index.Entries {
		if entry.Stage == 0 { // Only consider non-conflict entries
			stagedFiles[entry.FilePath] = entry
		}
	}

	// Compare commit tree with index to find staged changes
	for path, treeEntry := range commitTreeMap {
		indexEntry, inIndex := stagedFiles[path]
		if !inIndex {
			// File in commit but not in index = staged deletion
			status.StagedDeleted = append(status.StagedDeleted, path)
			status.IsClean = false
		} else if inIndex && indexEntry.SHA256 != treeEntry.Hash {
			// File in both, but different hash = modified in index
			status.StagedModified = append(status.StagedModified, path)
			status.IsClean = false
		}
	}

	// Check for new files in index (not in commit)
	for path := range stagedFiles {
		if _, inTree := commitTreeMap[path]; !inTree {
			status.NewFiles = append(status.NewFiles, path)
			status.IsClean = false
		}
	}

	// Compare index with working directory
	// Get a list of all files in the working directory
	var workingDirFiles []string
	err := filepath.Walk(repo.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip .vec directory
		if info.IsDir() && (info.Name() == ".vec" || strings.HasPrefix(path, filepath.Join(repo.Root, ".vec"))) {
			return filepath.SkipDir
		}
		// Skip directories
		if info.IsDir() {
			return nil
		}
		// Check if file should be ignored
		isIgnored, _ := utils.IsIgnored(repo.Root, path)
		if isIgnored {
			return nil
		}
		relPath, err := filepath.Rel(repo.Root, path)
		if err != nil {
			return err
		}
		workingDirFiles = append(workingDirFiles, relPath)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk working directory: %w", err)
	}

	// Create a map for easy lookup
	workingDirMap := make(map[string]bool)
	for _, path := range workingDirFiles {
		workingDirMap[path] = true
	}

	// Create a wait group for concurrent hash computation
	var wg sync.WaitGroup
	// Semaphore to limit concurrency
	semaphore := make(chan struct{}, 10)
	// Mutex for synchronizing access to the status
	var mutex sync.Mutex

	// Process each file in the index
	for path, entry := range stagedFiles {
		if _, inWorkingDir := workingDirMap[path]; !inWorkingDir {
			// File in index but not in working directory = deleted in working directory
			status.DeletedNotStaged = append(status.DeletedNotStaged, path)
			status.IsClean = false
		} else {
			// File in both, compare content
			wg.Add(1)
			semaphore <- struct{}{}
			go func(filePath string, indexEntry staging.IndexEntry) {
				defer wg.Done()
				defer func() { <-semaphore }()

				absPath := filepath.Join(repo.Root, filePath)
				content, err := os.ReadFile(absPath)
				if err != nil {
					return // Skip files we can't read
				}

				fileHash := utils.HashBytes("blob", content)
				if fileHash != indexEntry.SHA256 {
					mutex.Lock()
					status.ModifiedNotStaged = append(status.ModifiedNotStaged, filePath)
					status.IsClean = false
					mutex.Unlock()
				}
			}(path, entry)
		}
	}

	// Process each file in the working directory
	for path := range workingDirMap {
		if _, inIndex := stagedFiles[path]; !inIndex {
			// File in working directory but not in index = untracked
			status.Untracked = append(status.Untracked, path)
			status.IsClean = false
		}
	}

	// Wait for all concurrent hash computations to complete
	wg.Wait()

	// Sort all lists for consistent output
	sort.Strings(status.NewFiles)
	sort.Strings(status.StagedModified)
	sort.Strings(status.StagedDeleted)
	sort.Strings(status.Untracked)
	sort.Strings(status.ModifiedNotStaged)
	sort.Strings(status.DeletedNotStaged)
	sort.Strings(status.Conflicts)

	return status, nil
}



// buildCommitTreeMap recursively builds a map of file paths to tree entries from a tree object using Repository context
func buildCommitTreeMap(repo *core.Repository, tree *objects.TreeObject, parentPath string, treeMap map[string]objects.TreeEntry) {
	for _, entry := range tree.Entries {
		path := filepath.Join(parentPath, entry.Name)
		if entry.Type == "blob" {
			treeMap[path] = entry
		} else if entry.Type == "tree" {
			subTree, err := objects.GetTree(repo.Root, entry.Hash)
			if err != nil {
				// Skip subtrees we can't load
				continue
			}
			buildCommitTreeMap(repo, subTree, path, treeMap)
		}
	}
}



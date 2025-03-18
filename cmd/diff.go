package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/spf13/cobra"
)

var (
	cached   bool
	nameOnly bool
	color    bool
)

// diffCmd represents the diff command
var diffCmd = &cobra.Command{
	Use:   "diff [<options>] [<commit>] [--] [<path>...]",
	Short: "Show changes between commits, commit and working tree, etc",
	Long: `Show changes between the working tree and the staging area or the index and the latest commit.
When paths are specified, the diff is restricted to these paths.

Example:
  vec diff             # Show unstaged changes in the working tree
  vec diff --cached    # Show staged changes
  vec diff HEAD~1 HEAD # Show changes between the previous commit and HEAD
  vec diff branch1..branch2  # Show changes between two branches`,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}

		// Parse the diff type based on arguments and flags
		var src, dst string
		var paths []string

		if len(args) > 0 {
			// Check if the argument has the form of branch1..branch2
			if strings.Contains(args[0], "..") {
				parts := strings.Split(args[0], "..")
				if len(parts) == 2 {
					src = parts[0]
					dst = parts[1]
					paths = args[1:]
				}
			} else if len(args) >= 2 && isCommitOrBranch(repoRoot, args[0]) && isCommitOrBranch(repoRoot, args[1]) {
				// Two commits/branches specified
				src = args[0]
				dst = args[1]
				paths = args[2:]
			} else if len(args) >= 1 && isCommitOrBranch(repoRoot, args[0]) {
				// One commit/branch specified
				src = "HEAD"
				dst = args[0]
				paths = args[1:]
			} else {
				// No commits, just paths
				paths = args
			}
		}

		// Adjust source and destination based on flags
		if cached {
			// Compare staging area (index) to HEAD
			if src == "" && dst == "" {
				src = "HEAD"
				dst = "INDEX"
			}
		} else {
			// Compare working directory to staging area
			if src == "" && dst == "" {
				src = "INDEX"
				dst = "WORKTREE"
			}
		}

		return showDiff(repoRoot, src, dst, paths)
	},
}

// isCommitOrBranch checks if the given string is a valid commit hash or branch name
func isCommitOrBranch(repoRoot, ref string) bool {
	// First check if it's a branch
	branchPath := filepath.Join(repoRoot, ".vec", "refs", "heads", ref)
	if utils.FileExists(branchPath) {
		return true
	}

	// Special case for HEAD
	if ref == "HEAD" {
		return true
	}

	// Check if it's a commit
	// For simplicity, we'll just check if it's a valid SHA-256 hash (64 characters)
	if len(ref) == 64 {
		for _, c := range ref {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return false
			}
		}
		return true
	}

	// Check for HEAD~n syntax
	if strings.HasPrefix(ref, "HEAD~") {
		_, err := getCommitFromRef(repoRoot, ref)
		return err == nil
	}

	return false
}

// showDiff displays the differences between the two specified sources
func showDiff(repoRoot, src, dst string, paths []string) error {
	// Get the files from both sources
	srcFiles, err := getFilesFromRef(repoRoot, src)
	if err != nil {
		return fmt.Errorf("failed to get files from source: %w", err)
	}

	dstFiles, err := getFilesFromRef(repoRoot, dst)
	if err != nil {
		return fmt.Errorf("failed to get files from destination: %w", err)
	}

	// Filter by paths if specified
	if len(paths) > 0 {
		srcFiles = filterFilesByPaths(srcFiles, paths)
		dstFiles = filterFilesByPaths(dstFiles, paths)
	}

	// Find files that exist in either source
	allFiles := make(map[string]struct{})
	for file := range srcFiles {
		allFiles[file] = struct{}{}
	}
	for file := range dstFiles {
		allFiles[file] = struct{}{}
	}

	// Sort files for consistent output
	sortedFiles := make([]string, 0, len(allFiles))
	for file := range allFiles {
		sortedFiles = append(sortedFiles, file)
	}
	sort.Strings(sortedFiles)

	dmp := diffmatchpatch.New()
	diffFound := false

	// Display diffs for each file
	for _, file := range sortedFiles {
		srcContent, srcExists := srcFiles[file]
		dstContent, dstExists := dstFiles[file]

		if !srcExists {
			// File added
			if nameOnly {
				fmt.Printf("added: %s\n", file)
			} else {
				fmt.Printf("diff --vec a/%s b/%s\n", file, file)
				fmt.Printf("--- /dev/null\n")
				fmt.Printf("+++ b/%s\n", file)
				printNewFileContent(dstContent)
			}
			diffFound = true
		} else if !dstExists {
			// File removed
			if nameOnly {
				fmt.Printf("deleted: %s\n", file)
			} else {
				fmt.Printf("diff --vec a/%s b/%s\n", file, file)
				fmt.Printf("--- a/%s\n", file)
				fmt.Printf("+++ /dev/null\n")
				printDeletedFileContent(srcContent)
			}
			diffFound = true
		} else if srcContent != dstContent {
			// File modified
			if nameOnly {
				fmt.Printf("modified: %s\n", file)
			} else {
				diffs := dmp.DiffMain(srcContent, dstContent, false)
				if len(diffs) > 1 { // If there are actual differences
					fmt.Printf("diff --vec a/%s b/%s\n", file, file)
					fmt.Printf("--- a/%s\n", file)
					fmt.Printf("+++ b/%s\n", file)
					printUnifiedDiff(diffs)
				}
			}
			diffFound = true
		}
	}

	if !diffFound {
		fmt.Println("No changes.")
	}

	return nil
}

// getFilesFromRef retrieves the files and their contents from a specific ref
func getFilesFromRef(repoRoot, ref string) (map[string]string, error) {
	switch ref {
	case "WORKTREE":
		// Get files from working directory
		return getWorkingDirContents(repoRoot)
	case "INDEX":
		// Get files from staging area
		return getStagingAreaContents(repoRoot)
	default:
		// Get files from commit or branch
		commitHash, err := getCommitFromRef(repoRoot, ref)
		if err != nil {
			return nil, err
		}
		return getCommitContents(repoRoot, commitHash)
	}
}

// getWorkingDirContents returns a map of file paths to their contents in the working directory
func getWorkingDirContents(repoRoot string) (map[string]string, error) {
	files := make(map[string]string)

	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip .vec directory
		if info.IsDir() && path == filepath.Join(repoRoot, ".vec") {
			return filepath.SkipDir
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if the file is ignored
		isIgnored, err := utils.IsIgnored(repoRoot, path)
		if err != nil || isIgnored {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		files[relPath] = string(content)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// getStagingAreaContents returns a map of file paths to their contents in the staging area
func getStagingAreaContents(repoRoot string) (map[string]string, error) {
	files := make(map[string]string)

	// Load the index
	index, err := staging.LoadIndex(repoRoot)
	if err != nil {
		return nil, err
	}

	// For each entry in the index, get the file content
	for _, entry := range index.Entries {
		blob, err := objects.GetBlob(repoRoot, entry.SHA256)
		if err != nil {
			return nil, err
		}
		files[entry.FilePath] = string(blob)
	}

	return files, nil
}

// getCommitFromRef returns the commit hash for a given reference
func getCommitFromRef(repoRoot, ref string) (string, error) {
	// Handle HEAD
	if ref == "HEAD" {
		headFile := filepath.Join(repoRoot, ".vec", "HEAD")
		headContent, err := os.ReadFile(headFile)
		if err != nil {
			return "", err
		}
		headRef := strings.TrimSpace(string(headContent))

		// Check if HEAD is a reference or a direct commit hash
		if strings.HasPrefix(headRef, "ref: ") {
			// It's a reference, follow it
			refPath := strings.TrimPrefix(headRef, "ref: ")
			refFile := filepath.Join(repoRoot, ".vec", refPath)
			refContent, err := os.ReadFile(refFile)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(refContent)), nil
		} else {
			// It's a direct commit hash
			return headRef, nil
		}
	}

	// Handle HEAD~n syntax
	if strings.HasPrefix(ref, "HEAD~") {
		count := 1
		if len(ref) > 5 {
			fmt.Sscanf(ref[5:], "%d", &count)
		}

		// Get HEAD commit first
		headCommit, err := getCommitFromRef(repoRoot, "HEAD")
		if err != nil {
			return "", err
		}

		// Walk back count parents
		currentCommit := headCommit
		for i := 0; i < count; i++ {
			commit, err := objects.GetCommit(repoRoot, currentCommit)
			if err != nil {
				return "", err
			}
			if len(commit.Parents) == 0 {
				return "", fmt.Errorf("commit %s has no parent", currentCommit)
			}
			currentCommit = commit.Parents[0]
		}

		return currentCommit, nil
	}

	// Handle branch name
	branchPath := filepath.Join(repoRoot, ".vec", "refs", "heads", ref)
	if utils.FileExists(branchPath) {
		branchContent, err := os.ReadFile(branchPath)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(branchContent)), nil
	}

	// Assume it's a commit hash
	if len(ref) == 64 {
		return ref, nil
	}

	return "", fmt.Errorf("invalid reference: %s", ref)
}

// getCommitContents returns a map of file paths to their contents in a commit
func getCommitContents(repoRoot, commitHash string) (map[string]string, error) {
	files := make(map[string]string)

	// Get the commit
	commit, err := objects.GetCommit(repoRoot, commitHash)
	if err != nil {
		return nil, err
	}

	// Get the tree
	tree, err := objects.GetTree(repoRoot, commit.Tree)
	if err != nil {
		return nil, err
	}

	// Recursively get all entries in the tree
	err = walkTree(repoRoot, tree, "", files)
	if err != nil {
		return nil, err
	}

	return files, nil
}

// walkTree recursively walks through a tree and collects file contents
func walkTree(repoRoot string, tree *objects.TreeObject, prefix string, files map[string]string) error {
	for _, entry := range tree.Entries {
		path := filepath.Join(prefix, entry.Name)

		if entry.Type == "blob" {
			// It's a file, get its content
			blob, err := objects.GetBlob(repoRoot, entry.Hash)
			if err != nil {
				return err
			}
			files[path] = string(blob)
		} else if entry.Type == "tree" {
			// It's a directory, recurse into it
			subTree, err := objects.GetTree(repoRoot, entry.Hash)
			if err != nil {
				return err
			}
			err = walkTree(repoRoot, subTree, path, files)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// filterFilesByPaths filters a file map to only include files that match the given paths
func filterFilesByPaths(files map[string]string, paths []string) map[string]string {
	if len(paths) == 0 {
		return files
	}

	result := make(map[string]string)

	for file, content := range files {
		for _, path := range paths {
			if strings.HasPrefix(file, path) || path == file {
				result[file] = content
				break
			}
		}
	}

	return result
}

// printNewFileContent prints the content of a new file in diff format
func printNewFileContent(content string) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		fmt.Printf("+%s\n", line)
	}
}

// printDeletedFileContent prints the content of a deleted file in diff format
func printDeletedFileContent(content string) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		fmt.Printf("-%s\n", line)
	}
}

// printUnifiedDiff prints the differences in unified diff format
func printUnifiedDiff(diffs []diffmatchpatch.Diff) {
	lineNum := 1
	for _, diff := range diffs {
		text := diff.Text
		lines := strings.Split(text, "\n")

		switch diff.Type {
		case diffmatchpatch.DiffEqual:
			for _, line := range lines {
				fmt.Printf(" %s\n", line)
				lineNum++
			}
		case diffmatchpatch.DiffInsert:
			for _, line := range lines {
				fmt.Printf("+%s\n", line)
			}
		case diffmatchpatch.DiffDelete:
			for _, line := range lines {
				fmt.Printf("-%s\n", line)
				lineNum++
			}
		}
	}
}

func init() {
	rootCmd.AddCommand(diffCmd)

	// Add flags
	diffCmd.Flags().BoolVar(&cached, "cached", false, "View the changes you staged for the next commit")
	diffCmd.Flags().BoolVar(&nameOnly, "name-only", false, "Show only names of changed files")
	diffCmd.Flags().BoolVar(&color, "color", true, "Show colored diff")
}

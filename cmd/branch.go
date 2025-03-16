// cmd/branch.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var branchCmd = &cobra.Command{
	Use:   "branch",
	Short: "List, create, or delete branches",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}
		// If no arguments, list branches.
		if len(args) == 0 {
			return listBranches(repoRoot, cmd)
		}

		// If there is flag but no other arguments, return error
		list, _ := cmd.Flags().GetBool("list")
		deleteBranch, _ := cmd.Flags().GetString("delete")
		renameBranch, _ := cmd.Flags().GetString("rename")
		if list || deleteBranch != "" || renameBranch != "" {
			return fmt.Errorf("no argument for the defined flag")
		}
		// Otherwise, create a branch.
		return CreateBranch(repoRoot, args[0])
	},
}

func listBranches(repoRoot string, cmd *cobra.Command) error {
	// Get flag values
	deleteBranch, _ := cmd.Flags().GetString("delete")
	renameBranch, _ := cmd.Flags().GetString("rename")
	force, _ := cmd.Flags().GetBool("force")
	if deleteBranch != "" { // Delete branch
		if err := deleteBranchOp(repoRoot, deleteBranch, force); err != nil {
			return err
		}
	} else if renameBranch != "" {
		args := strings.Split(renameBranch, " ")
		if len(args) != 2 {
			return fmt.Errorf("rename requires two arguments")
		}
		if err := renameBranchOp(repoRoot, args[0], args[1]); err != nil {
			return err
		}
	} else { // List branches (default behavior)
		branchDir := filepath.Join(repoRoot, ".vec", "refs", "heads")
		entries, err := os.ReadDir(branchDir)
		if err != nil {
			return fmt.Errorf("failed to read branch directory: %w", err)
		}

		currentBranch, err := utils.GetCurrentBranch(repoRoot)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue // Skip subdirectories.
			}
			if entry.Name() == currentBranch {
				fmt.Printf("* %s\n", entry.Name()) // Mark the current branch.
			} else {
				fmt.Println(" ", entry.Name())
			}
		}
	}

	return nil
}

func CreateBranch(repoRoot string, branchName string) error {
	// Basic validation of branch name (you might want more robust checks).
	if strings.ContainsAny(branchName, " /\\~^:?*[]") {
		return fmt.Errorf("invalid branch name: %s", branchName)
	}

	branchPath := filepath.Join(repoRoot, ".vec", "refs", "heads", branchName)

	// Check if branch already exists.
	if utils.FileExists(branchPath) {
		return fmt.Errorf("a branch named '%s' already exists", branchName)
	}

	// Get current commit.
	currentCommit, err := utils.GetHeadCommit(repoRoot)
	if err != nil {
		return err
	}
	if currentCommit == "" {
		return fmt.Errorf("cannot create branch '%s' at this time", branchName)
	}

	// Create the branch file.
	if err := os.WriteFile(branchPath, []byte(currentCommit), 0644); err != nil {
		return fmt.Errorf("failed to create branch: %w", err)
	}
	return nil
}

func deleteBranchOp(repoRoot, branchName string, force bool) error {
	branchPath := filepath.Join(repoRoot, ".vec", "refs", "heads", branchName)

	//Check if branch exists
	if !utils.FileExists(branchPath) {
		return fmt.Errorf("branch '%s' not found", branchName)
	}

	currentBranch, err := utils.GetCurrentBranch(repoRoot)
	if err != nil {
		return err
	}
	// Check if it's the current branch.
	if branchName == currentBranch {
		return fmt.Errorf("cannot delete the currently checked-out branch '%s'", branchName)
	}

	// Check if the branch is fully merged
	if !force {
		// Get the commit hash that the branch points to
		branchCommitBytes, err := os.ReadFile(branchPath)
		if err != nil {
			return fmt.Errorf("failed to read branch file: %w", err)
		}
		branchCommit := strings.TrimSpace(string(branchCommitBytes))

		// Get the commit hash of the current branch
		currentCommit, err := utils.GetHeadCommit(repoRoot)
		if err != nil {
			return fmt.Errorf("failed to get current commit: %w", err)
		}

		// Check if the branch commit is an ancestor of the current commit
		isMerged, err := isAncestor(repoRoot, branchCommit, currentCommit)
		if err != nil {
			return fmt.Errorf("failed to check if branch is merged: %w", err)
		}

		if !isMerged {
			return fmt.Errorf("branch '%s' is not fully merged. Use --force to delete anyway", branchName)
		}
	}

	// Delete the branch file
	if err := os.Remove(branchPath); err != nil {
		return fmt.Errorf("failed to delete the branch '%s'", branchName)
	}
	return nil
}

func renameBranchOp(repoRoot, oldName, newName string) error {
	// Basic validation of branch name (you might want more robust checks).
	if strings.ContainsAny(newName, " /\\~^:?*[]") {
		return fmt.Errorf("invalid branch name: %s", newName)
	}
	oldBranchPath := filepath.Join(repoRoot, ".vec", "refs", "heads", oldName)
	newBranchPath := filepath.Join(repoRoot, ".vec", "refs", "heads", newName)

	// Check if the old branch exists
	if !utils.FileExists(oldBranchPath) {
		return fmt.Errorf("branch '%s' not found", oldName)
	}

	// Check if branch already exists.
	if utils.FileExists(newBranchPath) {
		return fmt.Errorf("a branch named '%s' already exists", newName)
	}

	//Rename the branch
	if err := os.Rename(oldBranchPath, newBranchPath); err != nil {
		return fmt.Errorf("failed to rename branch '%s' to '%s': %w", oldName, newName, err)
	}
	return nil
}

// IsAncestor checks if potentialAncestor is an ancestor of potentialDescendant
// Returns true if potentialAncestor is an ancestor of potentialDescendant, false otherwise
func isAncestor(repoRoot, potentialAncestor, potentialDescendant string) (bool, error) {
	// If they're the same commit, return true
	if potentialAncestor == potentialDescendant {
		return true, nil
	}

	// Get the descendant commit
	commit, err := objects.GetCommit(repoRoot, potentialDescendant)
	if err != nil {
		return false, fmt.Errorf("failed to get descendant commit: %w", err)
	}

	// BFS to find the ancestor
	visited := make(map[string]bool)
	queue := commit.Parents

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// Skip if already visited
		if visited[current] {
			continue
		}
		visited[current] = true

		// Check if this is the ancestor we're looking for
		if current == potentialAncestor {
			return true, nil
		}

		// Get the current commit's parents and add them to the queue
		currentCommit, err := objects.GetCommit(repoRoot, current)
		if err != nil {
			return false, fmt.Errorf("failed to get commit %s: %w", current, err)
		}

		queue = append(queue, currentCommit.Parents...)
	}

	// If we've traversed all reachable commits and haven't found the ancestor, return false
	return false, nil
}

func init() {
	rootCmd.AddCommand(branchCmd)
	branchCmd.Flags().BoolP("list", "l", false, "List all branches")
	branchCmd.Flags().StringP("delete", "d", "", "Delete a branch")
	branchCmd.Flags().StringP("rename", "m", "", "Rename a branch")
	branchCmd.Flags().BoolP("force", "f", false, "Force deletion of unmerged branches")
}

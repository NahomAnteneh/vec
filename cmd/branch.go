// cmd/branch.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/spf13/cobra"
)

// BranchHandler handles the branch command functionality
func BranchHandler(repo *core.Repository, args []string) error {
	// Retrieve the command through the closure in the factory function
	cmd := branchCmd

	// If no arguments, list branches.
	if len(args) == 0 {
		return listBranches(repo, cmd)
	}

	// If there is flag but no other arguments, return error
	list, _ := cmd.Flags().GetBool("list")
	deleteBranch, _ := cmd.Flags().GetString("delete")
	renameBranch, _ := cmd.Flags().GetString("rename")
	if list || deleteBranch != "" || renameBranch != "" {
		return core.RepositoryError("no argument for the defined flag", nil)
	}

	// Otherwise, create a branch.
	return CreateBranch(repo, args[0])
}

// listBranches lists all branches or performs branch operations based on flags
func listBranches(repo *core.Repository, cmd *cobra.Command) error {
	// Get flag values
	deleteBranch, _ := cmd.Flags().GetString("delete")
	renameBranch, _ := cmd.Flags().GetString("rename")
	force, _ := cmd.Flags().GetBool("force")

	if deleteBranch != "" { // Delete branch
		if err := deleteBranchOp(repo, deleteBranch, force); err != nil {
			return err
		}
	} else if renameBranch != "" {
		args := strings.Split(renameBranch, " ")
		if len(args) != 2 {
			return core.RepositoryError("rename requires two arguments", nil)
		}
		if err := renameBranchOp(repo, args[0], args[1]); err != nil {
			return err
		}
	} else { // List branches (default behavior)
		branchDir := filepath.Join(repo.RefsDir, "heads")
		entries, err := os.ReadDir(branchDir)
		if err != nil {
			return core.RefError("failed to read branch directory", err)
		}

		currentBranch, err := repo.GetCurrentBranch()
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

// CreateBranch creates a new branch pointing to the current HEAD commit
func CreateBranch(repo *core.Repository, branchName string) error {
	// Basic validation of branch name (you might want more robust checks).
	if strings.ContainsAny(branchName, " /\\~^:?*[]") {
		return core.RefError(fmt.Sprintf("invalid branch name: %s", branchName), nil)
	}

	branchPath := filepath.Join(repo.RefsDir, "heads", branchName)

	// Check if branch already exists.
	if core.FileExists(branchPath) {
		return core.AlreadyExistsError(core.ErrCategoryRef, fmt.Sprintf("branch '%s'", branchName))
	}

	// Get current commit.
	currentCommit, err := repo.ReadHead()
	if err != nil {
		return err
	}

	if currentCommit == "" {
		return core.RepositoryError(fmt.Sprintf("cannot create branch '%s' at this time", branchName), nil)
	}

	// Create the branch file by writing to the reference
	refPath := filepath.Join("refs", "heads", branchName)
	if err := repo.WriteRef(refPath, currentCommit); err != nil {
		return core.RefError("failed to create branch", err)
	}

	return nil
}

// deleteBranchOp deletes a branch, with force option to delete unmerged branches
func deleteBranchOp(repo *core.Repository, branchName string, force bool) error {
	branchPath := filepath.Join(repo.RefsDir, "heads", branchName)

	//Check if branch exists
	if !core.FileExists(branchPath) {
		return core.NotFoundError(core.ErrCategoryRef, fmt.Sprintf("branch '%s'", branchName))
	}

	currentBranch, err := repo.GetCurrentBranch()
	if err != nil {
		return err
	}

	// Check if it's the current branch.
	if branchName == currentBranch {
		return core.RepositoryError(fmt.Sprintf("cannot delete the currently checked-out branch '%s'", branchName), nil)
	}

	// Check if the branch is fully merged
	if !force {
		// Get the commit hash that the branch points to
		branchCommitBytes, err := core.ReadFileContent(branchPath)
		if err != nil {
			return core.RefError("failed to read branch file", err)
		}
		branchCommit := strings.TrimSpace(string(branchCommitBytes))

		// Get the commit hash of the current branch
		currentCommit, err := repo.ReadHead()
		if err != nil {
			return core.RefError("failed to get current commit", err)
		}

		// Check if the branch commit is an ancestor of the current commit
		isMerged, err := isAncestor(repo, branchCommit, currentCommit)
		if err != nil {
			return core.RepositoryError("failed to check if branch is merged", err)
		}

		if !isMerged {
			return core.RepositoryError(fmt.Sprintf("branch '%s' is not fully merged. Use --force to delete anyway", branchName), nil)
		}
	}

	// Delete the branch file
	if err := os.Remove(branchPath); err != nil {
		return core.RefError(fmt.Sprintf("failed to delete the branch '%s'", branchName), err)
	}
	return nil
}

// renameBranchOp renames a branch
func renameBranchOp(repo *core.Repository, oldName, newName string) error {
	// Basic validation of branch name (you might want more robust checks).
	if strings.ContainsAny(newName, " /\\~^:?*[]") {
		return core.RefError(fmt.Sprintf("invalid branch name: %s", newName), nil)
	}

	oldBranchPath := filepath.Join(repo.RefsDir, "heads", oldName)
	newBranchPath := filepath.Join(repo.RefsDir, "heads", newName)

	// Check if the old branch exists
	if !core.FileExists(oldBranchPath) {
		return core.NotFoundError(core.ErrCategoryRef, fmt.Sprintf("branch '%s'", oldName))
	}

	// Check if branch already exists.
	if core.FileExists(newBranchPath) {
		return core.AlreadyExistsError(core.ErrCategoryRef, fmt.Sprintf("branch '%s'", newName))
	}

	//Rename the branch
	if err := os.Rename(oldBranchPath, newBranchPath); err != nil {
		return core.RefError(fmt.Sprintf("failed to rename branch '%s' to '%s'", oldName, newName), err)
	}
	return nil
}

// isAncestor checks if potentialAncestor is an ancestor of potentialDescendant
// Returns true if potentialAncestor is an ancestor of potentialDescendant, false otherwise
func isAncestor(repo *core.Repository, potentialAncestor, potentialDescendant string) (bool, error) {
	// If they're the same commit, return true
	if potentialAncestor == potentialDescendant {
		return true, nil
	}

	// Get the descendant commit
	commit, err := objects.GetCommit(repo.Root, potentialDescendant)
	if err != nil {
		return false, core.ObjectError("failed to get descendant commit", err)
	}

	// Check if any parent of the descendant is the potential ancestor
	for _, parent := range commit.Parents {
		if parent == potentialAncestor {
			return true, nil
		}

		// Recursively check the parent's ancestors
		isAnc, err := isAncestor(repo, potentialAncestor, parent)
		if err != nil {
			return false, err
		}
		if isAnc {
			return true, nil
		}
	}

	return false, nil
}

// Store the command reference for use by the handler
var branchCmd *cobra.Command

func init() {
	// Create the command
	branchCmd = NewRepoCommand(
		"branch",
		"List, create, or delete branches",
		BranchHandler,
	)

	// Add flags
	branchCmd.Flags().BoolP("list", "l", false, "List branches")
	branchCmd.Flags().StringP("delete", "d", "", "Delete a branch")
	branchCmd.Flags().BoolP("force", "f", false, "Force delete a branch even if not merged")
	branchCmd.Flags().StringP("rename", "m", "", "Rename a branch with format 'oldname newname'")

	rootCmd.AddCommand(branchCmd)
}

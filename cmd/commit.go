package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
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
	index, err := staging.LoadIndex(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load index: %w", err)
	}

	// Verify there are changes to commit
	if index.IsClean(repoRoot) {
		return fmt.Errorf("nothing to commit, working tree clean")
	}

	// Retrieve author and committer info from config using the cascading approach
	authorName, err := GetCascadingConfigValue("user.name")
	if err != nil || authorName == "" {
		return fmt.Errorf("author name not configured; set it with 'vec config user.name <n>'")
	}

	authorEmail, err := GetCascadingConfigValue("user.email")
	if err != nil || authorEmail == "" {
		return fmt.Errorf("author email not configured; set it with 'vec config user.email <email>'")
	}

	author := fmt.Sprintf("%s <%s>", authorName, authorEmail)
	committer := author // For simplicity, assume committer is the same as author

	// Prompt for commit message if not provided
	if message == "" {
		// Try up to 3 times to get a non-empty message
		for attempts := 0; attempts < 3; attempts++ {
			if attempts == 0 {
				fmt.Print("Enter commit message: ")
			} else {
				fmt.Print("Commit message cannot be empty. Please try again: ")
			}

			reader := bufio.NewReader(os.Stdin)
			message, err = reader.ReadString('\n')

			// Check for read error
			if err != nil {
				return fmt.Errorf("error reading commit message: %w", err)
			}

			// Check if message is non-empty after trimming
			if trimmedMsg := strings.TrimSpace(message); trimmedMsg != "" {
				message = trimmedMsg
				break
			}

			// If we've reached the last attempt and still no message
			if attempts == 2 {
				return fmt.Errorf("aborting commit due to empty message")
			}
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
	treeHash, err := staging.CreateTreeFromIndex(repoRoot, index)
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

// init registers the commit command and its flags.
func init() {
	commitCmd.Flags().StringP("message", "m", "", "Commit message")
	rootCmd.AddCommand(commitCmd)
}

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/spf13/cobra"
)

// commitCmd defines the "commit" command with its usage and flags.
var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Record changes to the repository",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Find the repository
		repo, err := core.FindRepository()
		if err != nil {
			return fmt.Errorf("failed to find repository: %w", err)
		}

		message, _ := cmd.Flags().GetString("message")
		return CommitHandler(repo, message)
	},
}

// CommitHandler creates a new commit in the repository.
func CommitHandler(repo *core.Repository, message string) error {
	// Load the index to check for staged changes
	index, err := staging.LoadIndex(repo.Root)
	if err != nil {
		return fmt.Errorf("failed to load index: %w", err)
	}

	// Verify there are changes to commit
	if index.IsClean(repo.Root) {
		return fmt.Errorf("nothing to commit, working tree clean")
	}

	// Retrieve author and committer info from config
	authorName, err := repo.GetConfig("user.name")
	if err != nil || authorName == "" {
		return fmt.Errorf("author name not configured; set it with 'vec config user.name <n>'")
	}

	authorEmail, err := repo.GetConfig("user.email")
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
	parent, err := repo.ReadHead()
	if err != nil {
		return fmt.Errorf("failed to get parent commit: %w", err)
	}

	parents := []string{}
	if parent != "" {
		parents = append(parents, parent)
	}

	// Create tree object from the index
	treeHash, err := staging.CreateTreeFromIndex(repo.Root, index)
	if err != nil {
		return fmt.Errorf("failed to create tree from index: %w", err)
	}

	// Create the commit object
	commitHash, err := objects.CreateCommit(repo.Root, treeHash, parents, author, committer, message, timestamp)
	if err != nil {
		return fmt.Errorf("failed to create commit: %w", err)
	}

	// Update the branch pointer or HEAD if detached
	branch, err := repo.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if branch != "(HEAD detached)" {
		// Update branch reference
		refPath := filepath.Join("refs", "heads", branch)
		if err := repo.WriteRef(refPath, commitHash); err != nil {
			return fmt.Errorf("failed to update branch pointer: %w", err)
		}
	} else {
		// Update HEAD directly in detached mode
		if err := repo.UpdateHead(commitHash, false); err != nil {
			return fmt.Errorf("failed to update HEAD: %w", err)
		}
	}

	// Update reflog
	if err := updateReflogRepo(repo, parent, commitHash, branch, "commit", message); err != nil {
		return fmt.Errorf("failed to update reflog: %w", err)
	}

	// Display success message with short commit hash
	fmt.Printf("[(%s) %s] %s\n", branch, commitHash[:7], message)
	return nil
}

// updateReflogRepo updates the reflog with the given commit information
func updateReflogRepo(repo *core.Repository, oldCommit, newCommit, branch, action, message string) error {
	// Implementation details for updating reflog
	logFilePath := filepath.Join(repo.VecDir, "logs", "HEAD")

	// Create directory if it doesn't exist
	logDir := filepath.Dir(logFilePath)
	if err := core.EnsureDirExists(logDir); err != nil {
		return fmt.Errorf("failed to create reflog directory: %w", err)
	}

	// Format the reflog entry
	timestamp := time.Now().Unix()
	userName, err := repo.GetConfig("user.name")
	if err != nil || userName == "" {
		userName = "unknown"
	}
	userEmail, err := repo.GetConfig("user.email")
	if err != nil || userEmail == "" {
		userEmail = "unknown"
	}

	// Format: <old-sha> <new-sha> <author> <timestamp> <timezone> <message>
	logEntry := fmt.Sprintf("%s %s %s <%s> %d +0000 %s: %s\n",
		oldCommit,
		newCommit,
		userName,
		userEmail,
		timestamp,
		action,
		message)

	// Append to the reflog file
	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open reflog file: %w", err)
	}
	defer logFile.Close()

	if _, err := logFile.WriteString(logEntry); err != nil {
		return fmt.Errorf("failed to write to reflog: %w", err)
	}

	// If we're on a branch, also update the branch reflog
	if branch != "(HEAD detached)" {
		branchLogPath := filepath.Join(repo.VecDir, "logs", "refs", "heads", branch)
		branchLogDir := filepath.Dir(branchLogPath)

		if err := core.EnsureDirExists(branchLogDir); err != nil {
			return fmt.Errorf("failed to create branch reflog directory: %w", err)
		}

		branchLog, err := os.OpenFile(branchLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open branch reflog file: %w", err)
		}
		defer branchLog.Close()

		if _, err := branchLog.WriteString(logEntry); err != nil {
			return fmt.Errorf("failed to write to branch reflog: %w", err)
		}
	}

	return nil
}

// init registers the commit command and its flags.
func init() {
	commitCmd.Flags().StringP("message", "m", "", "Commit message")
	rootCmd.AddCommand(commitCmd)
}

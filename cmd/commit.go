package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Record changes to the repository",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}
		// Get message flag.
		message, _ := cmd.Flags().GetString("message")

		return commit(repoRoot, message)
	},
}

func commit(repoRoot string, message string) error {
	index, err := core.ReadIndex(repoRoot)
	if err != nil {
		return err
	}

	if index.IsClean() {
		return fmt.Errorf("nothing to commit, working tree clean")
	}

	// Get commit message (prompt if not provided via flag).
	if message == "" {
		fmt.Print("Enter commit message: ")
		reader := bufio.NewReader(os.Stdin)
		message, err = reader.ReadString('\n')
		if err != nil || strings.TrimSpace(message) == "" {
			return fmt.Errorf("aborting commit due to empty commit message")
		}
	}
	message = strings.TrimSpace(message) // Remove leading/trailing whitespace

	// Get author information from environment variables.
	author := os.Getenv("VEC_AUTHOR_NAME")
	if author == "" {
		author = "Unknown Author" // Fallback.
	}

	// Get timestamp.
	timestamp := time.Now().Unix()

	// Get parent commit(s).
	parent, err := utils.GetHeadCommit(repoRoot)
	if err != nil {
		return err
	}
	var parents []string
	if parent != "" {
		parents = []string{parent}
	}

	// Create tree object.
	treeHash, err := objects.CreateTree(repoRoot, index)
	if err != nil {
		return err
	}

	// Create commit object.
	// Now passing author directly.
	commitHash, err := objects.CreateCommit(repoRoot, treeHash, parents, author, message, timestamp)
	if err != nil {
		return err
	}

	// Update branch pointer.
	branch, err := utils.GetCurrentBranch(repoRoot)
	if err != nil {
		return err
	}

	if branch != "(HEAD detached)" {
		branchFile := filepath.Join(repoRoot, ".vec", "refs", "heads", branch)
		if err := os.WriteFile(branchFile, []byte(commitHash), 0644); err != nil {
			return fmt.Errorf("failed to update branch pointer: %w", err)
		}
	} else { // Update HEAD with commit hash if detached
		headFile := filepath.Join(repoRoot, ".vec", "HEAD")
		if err := os.WriteFile(headFile, []byte(commitHash), 0644); err != nil {
			return fmt.Errorf("failed to update HEAD: %w", err)
		}
	}

	// Update Reflog
	if err := updateReflog(repoRoot, parent, commitHash, branch); err != nil {
		return err
	}

	fmt.Printf("[(%s) %s] %s\n", branch, commitHash[:7], message) // Short hash for display.
	return nil
}

func updateReflog(repoRoot, prevCommit, commitHash, branch string) error {
	//Update HEAD reflog
	headReflogPath := filepath.Join(repoRoot, ".vec", "logs", "HEAD")
	headReflogContent := fmt.Sprintf("%s %s %s <%s> %d\t%s\n", prevCommit, commitHash, "Unknown", "Unknown", time.Now().Unix(), "commit: "+commitHash) //TODO: use committer and email
	f, err := os.OpenFile(headReflogPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to open HEAD reflog file: %w", err)
	}
	defer f.Close()

	_, err = f.WriteString(headReflogContent)
	if err != nil {
		return fmt.Errorf("failed to write to HEAD reflog file: %w", err)
	}

	if branch != "(HEAD detached)" {
		//Update branch reflog
		branchReflogPath := filepath.Join(repoRoot, ".vec", "logs", "refs", "heads", branch)
		if err := utils.EnsureDirExists(filepath.Dir(branchReflogPath)); err != nil {
			return err
		}
		branchReflogContent := fmt.Sprintf("%s %s %s <%s> %d\t%s\n", prevCommit, commitHash, "Unknown", "Unknown", time.Now().Unix(), "commit: "+commitHash) //TODO: use committer and email
		fb, err := os.OpenFile(branchReflogPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			return fmt.Errorf("failed to open branch reflog file: %w", err)
		}
		defer fb.Close()
		_, err = fb.WriteString(branchReflogContent)
		if err != nil {
			return fmt.Errorf("failed to write to branch reflog file: %w", err)
		}
	}

	return nil
}
func init() {
	rootCmd.AddCommand(commitCmd)
	commitCmd.Flags().StringP("message", "m", "", "Commit message")
}

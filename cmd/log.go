package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/objects"
)

// LogHandler handles the 'log' command for showing commit history
func LogHandler(repo *core.Repository, args []string) error {
	currentCommit, err := repo.ReadHead()
	if err != nil {
		return core.RefError("failed to get current commit", err)
	}

	// Iterate through the commit history.
	for currentCommit != "" {
		commit, err := objects.GetCommit(repo.Root, currentCommit)
		if err != nil {
			return core.ObjectError(fmt.Sprintf("failed to get commit %s", currentCommit), err)
		}

		fmt.Printf("commit:  %s\n", currentCommit)
		if len(commit.Parents) > 1 {
			fmt.Printf("Merge:  %s\n", strings.Join(commit.Parents, " "))
		}
		fmt.Printf("Author:  %s\n", commit.Author)
		fmt.Printf("Date:    %s\n", time.Unix(commit.Timestamp, 0).Format(time.RFC1123)) // Format the timestamp
		fmt.Println()
		fmt.Printf("    %s\n", commit.Message) // Indent the message
		fmt.Println()

		currentCommit = ""
		if len(commit.Parents) > 0 {
			currentCommit = commit.Parents[0] // Simple linear history for now.
		}
	}

	return nil
}

func init() {
	logCmd := NewRepoCommand(
		"log",
		"Show commit logs",
		LogHandler,
	)

	rootCmd.AddCommand(logCmd)
}

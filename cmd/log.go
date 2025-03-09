package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show commit logs",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}
		return log(repoRoot)
	},
}

func log(repoRoot string) error {
	currentCommit, err := utils.GetHeadCommit(repoRoot)
	if err != nil {
		return err
	}

	// Iterate through the commit history.
	for currentCommit != "" {
		commit, err := objects.GetCommit(repoRoot, currentCommit)
		if err != nil {
			return err
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
	rootCmd.AddCommand(logCmd)
}

// cmd/fetch.go
package cmd

import (
	"fmt"
	"os"

	"github.com/NahomAnteneh/vec/internal/config"
	"github.com/NahomAnteneh/vec/internal/remote"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var branch string

// fetchCmd represents the fetch command
var fetchCmd = &cobra.Command{
	Use:   "fetch [remote]",
	Short: "Fetch updates from a remote repository",
	Long:  `Downloads refs and objects from a remote repository, updating local tracking branches without merging.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Use default remote "origin" if none specified
		remoteName := "origin"
		if len(args) > 0 {
			remoteName = args[0]
		}

		// Load remote URL from config
		cfg, err := config.LoadConfig(repoRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		// Check if remote exists without actually using the URL here
		_, err = cfg.GetRemoteURL(remoteName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// If a branch is specified, fetch only that branch
		if branch != "" {
			if err := remote.FetchBranch(repoRoot, remoteName, branch); err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching branch %s from %s: %v\n", branch, remoteName, err)
				os.Exit(1)
			}
			fmt.Printf("Fetched branch %s from %s\n", branch, remoteName)
		} else {
			// Otherwise, fetch all refs
			if err := remote.Fetch(repoRoot, remoteName); err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching from %s: %v\n", remoteName, err)
				os.Exit(1)
			}
			fmt.Printf("Fetched updates from %s\n", remoteName)
		}
	},
}

func init() {
	fetchCmd.Flags().StringVarP(&branch, "branch", "b", "", "fetch a specific branch from the remote")
	rootCmd.AddCommand(fetchCmd)
}

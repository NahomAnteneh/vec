// cmd/pull.go
package cmd

import (
	"fmt"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/remote"
)

// PullHandler handles the 'pull' command for fetching and integrating changes
func PullHandler(repo *core.Repository, args []string) error {
	// Determine remote and branch
	remoteName := "origin"
	var branchName string

	if len(args) >= 1 {
		remoteName = args[0]
	}

	if len(args) >= 2 {
		branchName = args[1]
	}

	// Pull from remote
	if err := remote.PullRepo(repo, remoteName, branchName, false); err != nil {
		return core.RemoteError(fmt.Sprintf("failed to pull from remote '%s'", remoteName), err)
	}

	return nil
}

func init() {
	pullCmd := NewRepoCommand(
		"pull [<remote>] [<branch>]",
		"Fetch from and integrate with another repository or branch",
		PullHandler,
	)

	// Update help text
	pullCmd.Long = `Fetch from and integrate with another repository or branch.
If no remote is specified, 'origin' is used.
If no branch is specified, the current branch is used.`

	rootCmd.AddCommand(pullCmd)
}

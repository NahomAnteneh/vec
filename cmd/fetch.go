// cmd/fetch.go
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/config"
	"github.com/NahomAnteneh/vec/internal/remote"
	"github.com/spf13/cobra"
)

var (
	// Fetch command options
	fetchAllRemotes bool
	fetchPrune      bool
	fetchQuiet      bool
	fetchVerbose    bool
	fetchForce      bool
	fetchDepth      int
	fetchTags       bool
	fetchBranch     string
	fetchDryRun     bool
	fetchProgress   bool
)

// FetchHandler handles the fetch command logic using the repository context
func FetchHandler(repo *core.Repository, args []string) error {
	// Start time measurement for performance reporting
	startTime := time.Now()

	// Load configuration
	cfg, err := config.LoadConfigRepo(repo)
	if err != nil {
		return core.ConfigError("error loading config", err)
	}

	// Determine which remotes to fetch
	remotesToFetch := []string{}
	if fetchAllRemotes {
		// Get all configured remotes
		for name := range cfg.Remotes {
			remotesToFetch = append(remotesToFetch, name)
		}
		if len(remotesToFetch) == 0 {
			return core.RemoteError("no remotes configured", nil)
		}
	} else {
		// Use specified remote or default to "origin"
		remoteName := "origin"
		if len(args) > 0 {
			remoteName = args[0]
		}

		// Check if remote exists
		if _, err := cfg.GetRemoteURL(remoteName); err != nil {
			return core.RemoteError(fmt.Sprintf("remote '%s' does not exist. Use 'vec remote add' to add a new remote", remoteName), nil)
		}
		remotesToFetch = []string{remoteName}
	}

	// Track fetch results for summary
	fetchResults := make(map[string]fetchResult)
	anySuccess := false

	// Create fetch options
	fetchOptions := remote.FetchOptions{
		Quiet:     fetchQuiet,
		Verbose:   fetchVerbose,
		Force:     fetchForce,
		Depth:     fetchDepth,
		FetchTags: fetchTags,
		Branch:    fetchBranch,
		DryRun:    fetchDryRun,
		Progress:  fetchProgress,
		Prune:     fetchPrune,
	}

	// Fetch from each remote
	for _, remoteName := range remotesToFetch {
		result := fetchResult{
			RemoteName: remoteName,
			Success:    false,
		}

		// Show starting message
		if !fetchQuiet {
			fmt.Printf("Fetching from remote '%s'...\n", remoteName)
		}

		// If branch is specified, fetch only that branch
		var fetchErr error
		if fetchBranch != "" {
			if fetchVerbose {
				fmt.Printf("Fetching branch '%s' from remote '%s'\n", fetchBranch, remoteName)
			}
			fetchErr = remote.FetchBranchWithOptionsRepo(repo, remoteName, fetchBranch, fetchOptions)
		} else {
			// Otherwise, fetch all refs
			fetchErr = remote.FetchWithOptionsRepo(repo, remoteName, fetchOptions)
		}

		if fetchErr != nil {
			result.Error = fetchErr
			if !fetchQuiet {
				fmt.Fprintf(os.Stderr, "Error fetching from '%s': %v\n", remoteName, fetchErr)
			}
		} else {
			result.Success = true
			anySuccess = true
			if !fetchQuiet {
				fmt.Printf("Successfully fetched from '%s'\n", remoteName)
			}
		}

		fetchResults[remoteName] = result
	}

	// Display summary if fetching from multiple remotes or in verbose mode
	if (len(remotesToFetch) > 1 || fetchVerbose) && !fetchQuiet {
		displayFetchSummary(fetchResults)
	}

	// Show completion timing info
	if !fetchQuiet {
		duration := time.Since(startTime).Round(time.Millisecond)
		fmt.Printf("Fetch completed in %v\n", duration)
	}

	// If no fetch operations succeeded, return error
	if !anySuccess && len(remotesToFetch) > 0 {
		return core.RemoteError("failed to fetch from any remote", nil)
	}

	return nil
}

// Result type for tracking fetch operations
type fetchResult struct {
	RemoteName string
	Success    bool
	Error      error
}

// Display a summary of fetch operations
func displayFetchSummary(results map[string]fetchResult) {
	fmt.Println("\nFetch Summary:")
	fmt.Println("------------------------------------")

	successCount := 0
	failureCount := 0

	for _, result := range results {
		status := "SUCCESS"
		details := ""
		if !result.Success {
			status = "FAILED"
			details = fmt.Sprintf(" - %v", result.Error)
			failureCount++
		} else {
			successCount++
		}
		fmt.Printf("  %s: %s%s\n", result.RemoteName, status, details)
	}

	fmt.Println("------------------------------------")
	fmt.Printf("Total: %d successful, %d failed\n", successCount, failureCount)
}

func init() {
	fetchCmd := NewRepoCommand(
		"fetch [remote]",
		"Fetch updates from a remote repository",
		FetchHandler,
	)

	fetchCmd.Long = `Downloads refs and objects from a remote repository, updating local tracking branches without merging.

Examples:
  vec fetch                     # Fetch from default remote (origin)
  vec fetch upstream            # Fetch from a specific remote
  vec fetch --all               # Fetch from all configured remotes
  vec fetch --branch=feature    # Fetch only a specific branch
  vec fetch --prune             # Remove deleted remote branches
  vec fetch --verbose           # Show detailed fetch information
  vec fetch --depth=1           # Shallow fetch with depth 1
  vec fetch --tags              # Fetch all tags
`
	fetchCmd.Args = cobra.MaximumNArgs(1)

	// Add fetch options
	fetchCmd.Flags().BoolVar(&fetchAllRemotes, "all", false, "Fetch from all remotes")
	fetchCmd.Flags().BoolVar(&fetchPrune, "prune", false, "Remove remote-tracking branches that no longer exist on the remote")
	fetchCmd.Flags().BoolVar(&fetchQuiet, "quiet", false, "Suppress all output")
	fetchCmd.Flags().BoolVar(&fetchVerbose, "verbose", false, "Be verbose")
	fetchCmd.Flags().BoolVar(&fetchForce, "force", false, "Force update of local branches")
	fetchCmd.Flags().IntVar(&fetchDepth, "depth", 0, "Create a shallow clone with a history truncated to the specified number of commits")
	fetchCmd.Flags().BoolVar(&fetchTags, "tags", false, "Fetch all tags and associated objects")
	fetchCmd.Flags().StringVar(&fetchBranch, "branch", "", "Fetch a specific branch")
	fetchCmd.Flags().BoolVar(&fetchDryRun, "dry-run", false, "Show what would be done, without making actual changes")
	fetchCmd.Flags().BoolVar(&fetchProgress, "progress", true, "Show progress during fetch")

	rootCmd.AddCommand(fetchCmd)
}

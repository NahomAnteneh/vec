// internal/remote/pull.go
package remote

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/config"
	vechttp "github.com/NahomAnteneh/vec/internal/remote/http"
)

// Pull fetches changes from a remote repository and updates the current branch
// Legacy function that uses the repository root path
func Pull(repoRoot, remoteName, branchName string, verbose bool) error {
	// Create a repository context
	repo := core.NewRepository(repoRoot)
	return PullRepo(repo, remoteName, branchName, verbose)
}

// PullRepo fetches changes from a remote repository using the Repository context
func PullRepo(repo *core.Repository, remoteName, branchName string, verbose bool) error {
	// Load configuration
	cfg, err := config.LoadConfig(repo.Root)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Get remote URL
	remoteURL, err := cfg.GetRemoteURL(remoteName)
	if err != nil {
		return fmt.Errorf("failed to get remote URL: %w", err)
	}

	if remoteURL == "" {
		return fmt.Errorf("remote '%s' not found or has no URL configured", remoteName)
	}

	// Fetch the latest changes from remote
	log.Printf("Fetching from %s/%s", remoteName, branchName)

	// First check if we have authentication token
	_, err = GetAuthToken(remoteName)
	if err != nil {
		// If no token, attempt to get user credentials
		log.Printf("No auth token found. Please login first with 'vec login %s'", remoteName)
		return fmt.Errorf("authentication required: %w", err)
	}

	// Create HTTP client
	client := vechttp.NewClient(remoteURL, remoteName, cfg)

	// Fetch references to see what's available
	refs, err := client.FetchRefs()
	if err != nil {
		// If unauthorized, try to refresh token
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "unauthorized") {
			log.Printf("Token expired, attempting to refresh...")
			_, err := RefreshAuthToken(remoteName)
			if err != nil {
				log.Printf("Token refresh failed: %v", err)
				log.Printf("Please login again with 'vec login %s'", remoteName)
				return fmt.Errorf("authentication failed: %w", err)
			}

			// Retry with new token
			refs, err = client.FetchRefs()
			if err != nil {
				return fmt.Errorf("failed to fetch refs after token refresh: %w", err)
			}
		} else {
			return fmt.Errorf("failed to fetch refs: %w", err)
		}
	}

	// Check if the target branch exists on the remote
	targetRef := fmt.Sprintf("refs/heads/%s", branchName)
	remoteCommitID := ""

	// Refs is a map[string]string, not a slice of structs
	for refName, commitID := range refs {
		if refName == targetRef {
			remoteCommitID = commitID
			break
		}
	}

	if remoteCommitID == "" {
		return fmt.Errorf("branch '%s' not found on remote '%s'", branchName, remoteName)
	}

	// Get the current commit ID for the branch
	branchPath := filepath.Join(repo.Root, ".vec", "refs", "heads", branchName)
	localCommitID := ""

	// Check if branch file exists
	if fileInfo, err := os.Stat(branchPath); err == nil && !fileInfo.IsDir() {
		// Read the commit ID from the branch file
		data, err := os.ReadFile(branchPath)
		if err != nil {
			return fmt.Errorf("failed to read branch file: %w", err)
		}
		localCommitID = strings.TrimSpace(string(data))
	}

	// If already up to date, nothing to do
	if localCommitID == remoteCommitID {
		log.Printf("Branch '%s' is already up to date with '%s/%s'", branchName, remoteName, branchName)
		return nil
	}

	// Fetch objects from remote
	if err := fetchObjectsForPullRepo(repo, remoteURL, remoteName, localCommitID, remoteCommitID, cfg); err != nil {
		return fmt.Errorf("failed to fetch objects: %w", err)
	}

	// Update the branch reference
	if err := os.MkdirAll(filepath.Dir(branchPath), 0755); err != nil {
		return fmt.Errorf("failed to create branch directory: %w", err)
	}

	if err := os.WriteFile(branchPath, []byte(remoteCommitID), 0644); err != nil {
		return fmt.Errorf("failed to update branch reference: %w", err)
	}

	log.Printf("Successfully updated branch '%s' to commit %s", branchName, remoteCommitID)
	return nil
}

// Legacy function that uses the repository root path
func fetchObjectsForPull(repoRoot, remoteURL, remoteName, localCommit, remoteCommit string, cfg *config.Config) error {
	repo := core.NewRepository(repoRoot)
	return fetchObjectsForPullRepo(repo, remoteURL, remoteName, localCommit, remoteCommit, cfg)
}

// fetchObjectsForPullRepo fetches objects from the remote using Repository context
func fetchObjectsForPullRepo(repo *core.Repository, remoteURL, remoteName, localCommit, remoteCommit string, cfg *config.Config) error {
	// Create a map for negotiation
	remoteRefs := map[string]string{"want": remoteCommit}
	localRefs := map[string]string{}

	if localCommit != "" {
		localRefs["have"] = localCommit
	}

	// Negotiate which objects we need to fetch
	objectsList, err := vechttp.NegotiateFetch(remoteURL, remoteName, remoteRefs, localRefs, cfg)
	if err != nil {
		return fmt.Errorf("failed to negotiate objects: %w", err)
	}

	// If no objects to fetch, we're done
	if len(objectsList) == 0 {
		fmt.Println("No new objects to fetch")
		return nil
	}

	// Fetch packfile containing the objects
	packfile, err := vechttp.FetchPackfile(remoteURL, remoteName, objectsList, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch packfile: %w", err)
	}

	// Process the packfile to extract objects
	if err := unpackPackfileRepo(repo, packfile); err != nil {
		return fmt.Errorf("failed to unpack packfile: %w", err)
	}

	return nil
}

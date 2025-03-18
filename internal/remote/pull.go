// internal/remote/pull.go
package remote

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/internal/config"
	vechttp "github.com/NahomAnteneh/vec/internal/remote/http"
	"github.com/NahomAnteneh/vec/utils"
)

// Pull fetches changes from a remote repository and updates the current branch
func Pull(repoRoot, remoteName, branchName string, verbose bool) error {
	// Load configuration
	cfg, err := config.LoadConfig(repoRoot)
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
	branchPath := filepath.Join(repoRoot, ".vec", "refs", "heads", branchName)
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

	// Continue with the existing fetch implementation
	// ...

	return nil
}

// getCurrentBranchForPull gets the name of the current branch
func getCurrentBranchForPull(repoRoot string) (string, error) {
	headFile := filepath.Join(repoRoot, ".vec", "HEAD")
	if !utils.FileExists(headFile) {
		return "", fmt.Errorf("HEAD file not found")
	}

	content, err := os.ReadFile(headFile)
	if err != nil {
		return "", fmt.Errorf("failed to read HEAD file: %w", err)
	}

	headRef := strings.TrimSpace(string(content))
	if !strings.HasPrefix(headRef, "ref: refs/heads/") {
		return "", fmt.Errorf("HEAD is detached")
	}

	return strings.TrimPrefix(headRef, "ref: refs/heads/"), nil
}

// getLocalBranchCommitForPull gets the commit hash of a local branch
func getLocalBranchCommitForPull(repoRoot, branchName string) (string, error) {
	branchFile := filepath.Join(repoRoot, ".vec", "refs", "heads", branchName)
	if !utils.FileExists(branchFile) {
		return "", os.ErrNotExist
	}

	content, err := os.ReadFile(branchFile)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(content)), nil
}

// getRemoteBranchCommitForPull gets the commit hash of a remote branch
func getRemoteBranchCommitForPull(remoteURL, remoteName, branchName string, cfg *config.Config) (string, error) {
	return vechttp.GetBranchCommit(remoteURL, remoteName, branchName, cfg)
}

// fetchObjectsForPull fetches objects from the remote
func fetchObjectsForPull(repoRoot, remoteURL, remoteName, localCommit, remoteCommit string, cfg *config.Config) error {
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
	if err := unpackPackfile(repoRoot, packfile); err != nil {
		return fmt.Errorf("failed to unpack packfile: %w", err)
	}

	return nil
}

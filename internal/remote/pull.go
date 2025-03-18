// internal/remote/pull.go
package remote

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/internal/config"
	vechttp "github.com/NahomAnteneh/vec/internal/remote/http"
	"github.com/NahomAnteneh/vec/utils"
)

// Pull updates the local repository with changes from a remote
func Pull(repoRoot, remoteName, branchName string) error {
	// Load config
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Get remote URL
	remoteURL, err := cfg.GetRemoteURL(remoteName)
	if err != nil {
		return fmt.Errorf("failed to get remote URL: %w", err)
	}

	// Determine branch
	if branchName == "" {
		// Get current branch
		currentBranch, err := getCurrentBranchForPull(repoRoot)
		if err != nil {
			return fmt.Errorf("failed to get current branch: %w", err)
		}
		branchName = currentBranch
	}

	// Get local branch commit
	localCommitHash, err := getLocalBranchCommitForPull(repoRoot, branchName)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to get local branch commit: %w", err)
	}

	// Get remote branch commit
	remoteCommitHash, err := getRemoteBranchCommitForPull(remoteURL, remoteName, branchName, cfg)
	if err != nil {
		return fmt.Errorf("failed to get remote branch commit: %w", err)
	}

	// Check if already up to date
	if localCommitHash == remoteCommitHash && localCommitHash != "" {
		fmt.Printf("Already up to date. Branch %s is at commit %s\n", branchName, localCommitHash)
		return nil
	}

	// Fetch objects from remote
	if err := fetchObjectsForPull(repoRoot, remoteURL, remoteName, localCommitHash, remoteCommitHash, cfg); err != nil {
		return fmt.Errorf("failed to fetch objects: %w", err)
	}

	// Update local branch
	branchFile := filepath.Join(repoRoot, ".vec", "refs", "heads", branchName)
	if err := os.MkdirAll(filepath.Dir(branchFile), 0755); err != nil {
		return fmt.Errorf("failed to create branch directory: %w", err)
	}

	if err := os.WriteFile(branchFile, []byte(remoteCommitHash), 0644); err != nil {
		return fmt.Errorf("failed to update branch: %w", err)
	}

	fmt.Printf("Successfully pulled branch %s from %s\n", branchName, remoteName)
	fmt.Printf("Updated to commit %s\n", remoteCommitHash)
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

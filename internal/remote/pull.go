// internal/remote/pull.go
package remote

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/config"
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
	url := fmt.Sprintf("%s/refs/heads/%s", remoteURL, branchName)
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication headers if available
	ApplyAuthHeaders(req, remoteName, cfg)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to contact remote: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("branch %s not found on remote", branchName)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get remote branch with status %d", resp.StatusCode)
	}

	var result struct {
		Commit string `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Commit, nil
}

// fetchObjectsForPull fetches objects from the remote
func fetchObjectsForPull(repoRoot, remoteURL, remoteName, localCommit, remoteCommit string, cfg *config.Config) error {
	client := &http.Client{Timeout: 30 * time.Second}
	packURL := fmt.Sprintf("%s/fetch", remoteURL)

	// Create request with required objects
	requestData := map[string]interface{}{
		"want": []string{remoteCommit},
		"have": []string{},
	}

	if localCommit != "" {
		requestData["have"] = []string{localCommit}
	}

	requestJSON, err := json.Marshal(requestData)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", packURL, strings.NewReader(string(requestJSON)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Add authentication headers if available
	ApplyAuthHeaders(req, remoteName, cfg)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch objects: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	// Process the packfile
	// In a real implementation, this would unpack the Git packfile format
	var packfile map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&packfile); err != nil {
		return fmt.Errorf("failed to decode packfile: %w", err)
	}

	// Extract and write objects
	objectsData, ok := packfile["objects"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid packfile format")
	}

	for hash, data := range objectsData {
		objectData, ok := data.(string)
		if !ok {
			continue
		}

		objectPath := filepath.Join(repoRoot, ".vec", "objects", hash[:2], hash[2:])
		if err := os.MkdirAll(filepath.Dir(objectPath), 0755); err != nil {
			return fmt.Errorf("failed to create object directory: %w", err)
		}

		if err := os.WriteFile(objectPath, []byte(objectData), 0644); err != nil {
			return fmt.Errorf("failed to write object %s: %w", hash, err)
		}
	}

	return nil
}

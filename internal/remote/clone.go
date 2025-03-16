// internal/remote/clone.go
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
)

// Clone initializes a new repository from a remote URL
func Clone(url, destPath string, auth string) error {
	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Check if destination is empty
	entries, err := os.ReadDir(destPath)
	if err != nil {
		return fmt.Errorf("failed to read destination directory: %w", err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("destination directory is not empty")
	}

	// Initialize repository
	repoPath := filepath.Join(destPath, ".vec")
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return fmt.Errorf("failed to create repository directory: %w", err)
	}

	// Create required directories
	dirs := []string{"objects", "refs/heads", "refs/tags"}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(repoPath, dir), 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Set up default branch (main)
	headPath := filepath.Join(repoPath, "HEAD")
	if err := os.WriteFile(headPath, []byte("ref: refs/heads/main\n"), 0644); err != nil {
		return fmt.Errorf("failed to create HEAD file: %w", err)
	}

	// Set up config
	cfg := config.Config{}

	// Add remote as origin
	remoteName := "origin"
	err = cfg.AddRemote(remoteName, url)
	if err != nil {
		return fmt.Errorf("failed to add remote: %w", err)
	}

	// Set authentication if provided
	if auth != "" {
		err = cfg.SetRemoteAuth(remoteName, auth)
		if err != nil {
			return fmt.Errorf("failed to set authentication: %w", err)
		}
	}

	// Save config
	cfg = *config.NewConfig(destPath)
	err = cfg.AddRemote(remoteName, url)
	if err != nil {
		return fmt.Errorf("failed to add remote: %w", err)
	}
	if auth != "" {
		err = cfg.SetRemoteAuth(remoteName, auth)
		if err != nil {
			return fmt.Errorf("failed to set authentication: %w", err)
		}
	}
	if err := cfg.Write(); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Fetch from remote
	refs, err := fetchRemoteRefsForClone(url, remoteName, &cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch remote refs: %w", err)
	}

	if len(refs) == 0 {
		fmt.Println("Remote repository is empty")
		return nil
	}

	// Find main branch or another default branch
	var defaultBranch string
	var defaultCommit string

	// First try main
	for ref, commit := range refs {
		if ref == "refs/heads/main" {
			defaultBranch = "main"
			defaultCommit = commit
			break
		}
	}

	// If main not found, try master
	if defaultBranch == "" {
		for ref, commit := range refs {
			if ref == "refs/heads/master" {
				defaultBranch = "master"
				defaultCommit = commit
				break
			}
		}
	}

	// If neither main nor master, use first branch found
	if defaultBranch == "" {
		for ref, commit := range refs {
			if strings.HasPrefix(ref, "refs/heads/") {
				defaultBranch = strings.TrimPrefix(ref, "refs/heads/")
				defaultCommit = commit
				break
			}
		}
	}

	if defaultBranch == "" {
		return fmt.Errorf("no branches found in remote repository")
	}

	// Update HEAD to point to the default branch
	if defaultBranch != "main" {
		if err := os.WriteFile(headPath, []byte(fmt.Sprintf("ref: refs/heads/%s\n", defaultBranch)), 0644); err != nil {
			return fmt.Errorf("failed to update HEAD: %w", err)
		}
	}

	// Fetch objects from remote
	if err := fetchAndProcessPackfile(destPath, url, defaultCommit, refs, remoteName, &cfg); err != nil {
		return fmt.Errorf("failed to fetch objects: %w", err)
	}

	// Update local refs
	for ref, commit := range refs {
		if strings.HasPrefix(ref, "refs/heads/") {
			branchName := strings.TrimPrefix(ref, "refs/heads/")
			branchPath := filepath.Join(repoPath, "refs", "heads", branchName)
			if err := os.WriteFile(branchPath, []byte(commit), 0644); err != nil {
				return fmt.Errorf("failed to create branch %s: %w", branchName, err)
			}
		}
	}

	fmt.Printf("Cloned repository into %s\n", destPath)
	fmt.Printf("Default branch: %s\n", defaultBranch)

	return nil
}

// fetchAndProcessPackfile fetches objects from the remote
func fetchAndProcessPackfile(repoPath, url, commit string, refs map[string]string, remoteName string, cfg *config.Config) error {
	client := &http.Client{Timeout: 30 * time.Second}
	packURL := fmt.Sprintf("%s/fetch", url)

	// Create request with required objects
	requestData := map[string]interface{}{
		"want": []string{commit},
		"have": []string{},
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

	// Add authentication headers if provided (but not required)
	// applyAuthHeadersToRequest(req, remoteName, cfg)

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

		objectPath := filepath.Join(repoPath, ".vec", "objects", hash[:2], hash[2:])
		if err := os.MkdirAll(filepath.Dir(objectPath), 0755); err != nil {
			return fmt.Errorf("failed to create object directory: %w", err)
		}

		if err := os.WriteFile(objectPath, []byte(objectData), 0644); err != nil {
			return fmt.Errorf("failed to write object %s: %w", hash, err)
		}
	}

	return nil
}

// fetchRemoteRefsForClone retrieves the branch refs for cloning (no auth required)
func fetchRemoteRefsForClone(remoteURL, remoteName string, cfg *config.Config) (map[string]string, error) {
	url := fmt.Sprintf("%s/refs/heads", remoteURL)
	client := &http.Client{Timeout: 10 * time.Second}

	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Add authentication headers if provided (but not required)
		// applyAuthHeadersToRequest(req, remoteName, cfg)

		resp, err := client.Do(req)
		if err != nil {
			if attempt == 3 {
				return nil, fmt.Errorf("cannot contact remote after %d attempts: %w", attempt, err)
			}
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("refs fetch failed with status %d", resp.StatusCode)
		}

		var refs map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&refs); err != nil {
			return nil, fmt.Errorf("failed to decode refs: %w", err)
		}
		return refs, nil
	}
	return nil, fmt.Errorf("unexpected error in fetchRemoteRefsForClone")
}

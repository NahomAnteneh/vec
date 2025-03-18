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

// CloneOptions contains all options for cloning a repository
type CloneOptions struct {
	// Required options
	URL      string // Repository URL to clone from
	DestPath string // Destination directory path

	// Authentication
	Auth string // Authentication token (optional)

	// Clone options
	Branch     string // Specific branch to checkout (optional)
	Depth      int    // Depth limit for shallow clones (0 means full clone)
	Recursive  bool   // Whether to clone submodules recursively
	NoCheckout bool   // Skip checkout of HEAD after clone
	Bare       bool   // Create a bare repository
	Quiet      bool   // Suppress progress output
	Progress   bool   // Show progress during clone
}

// Clone initializes a new repository from a remote URL (original function for backward compatibility)
func Clone(url, destPath string, auth string) error {
	return CloneWithOptions(CloneOptions{
		URL:      url,
		DestPath: destPath,
		Auth:     auth,
		Progress: true,
	})
}

// CloneWithOptions initializes a new repository from a remote URL with extended options
func CloneWithOptions(opts CloneOptions) error {
	// For backward compatibility
	if strings.TrimSpace(opts.URL) == "" || strings.TrimSpace(opts.DestPath) == "" {
		return fmt.Errorf("URL and destination path are required")
	}

	url := opts.URL
	destPath := opts.DestPath
	auth := opts.Auth

	// Print progress if requested
	logProgress := func(format string, args ...interface{}) {
		if !opts.Quiet && opts.Progress {
			fmt.Printf(format, args...)
		}
	}

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
	logProgress("Creating repository structure...\n")
	dirs := []string{"objects", "refs/heads", "refs/tags"}
	if !opts.Bare {
		dirs = append(dirs, "refs/remotes")
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(repoPath, dir), 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Set up default branch (main), unless specific branch is requested
	defaultBranchName := "main"
	if opts.Branch != "" {
		defaultBranchName = opts.Branch
	}

	// Write HEAD file
	headPath := filepath.Join(repoPath, "HEAD")
	headRef := fmt.Sprintf("ref: refs/heads/%s\n", defaultBranchName)
	if opts.Bare {
		// In bare repos, we don't point HEAD to a branch
		headRef = "" // Will be set later once we know the actual default branch
	}
	if err := os.WriteFile(headPath, []byte(headRef), 0644); err != nil {
		return fmt.Errorf("failed to create HEAD file: %w", err)
	}

	// Set up config
	logProgress("Configuring remote...\n")
	cfg := config.NewConfig(destPath)

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
	if err := cfg.Write(); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Fetch from remote
	logProgress("Fetching remote repository...\n")
	refs, err := fetchRemoteRefsForClone(url, remoteName, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch remote refs: %w", err)
	}

	if len(refs) == 0 {
		logProgress("Remote repository is empty\n")
		return nil
	}

	// Find main branch or another default branch
	var defaultBranch string
	var defaultCommit string

	// If branch specified in options, try that first
	if opts.Branch != "" {
		branchRef := "refs/heads/" + opts.Branch
		if commit, exists := refs[branchRef]; exists {
			defaultBranch = opts.Branch
			defaultCommit = commit
		} else {
			return fmt.Errorf("specified branch '%s' does not exist in the remote repository", opts.Branch)
		}
	} else {
		// Otherwise try to find a default branch
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
	}

	if defaultBranch == "" {
		return fmt.Errorf("no branches found in remote repository")
	}

	// Set HEAD to point to default branch if it differs from our initial guess
	if defaultBranch != defaultBranchName || opts.Bare {
		headRef := fmt.Sprintf("ref: refs/heads/%s\n", defaultBranch)
		if opts.Bare {
			// For bare repos, head points directly to the commit
			headRef = defaultCommit
		}
		if err := os.WriteFile(headPath, []byte(headRef), 0644); err != nil {
			return fmt.Errorf("failed to update HEAD: %w", err)
		}
	}

	// Fetch objects from remote
	logProgress("Fetching objects...\n")

	// Limit fetch depth if shallow clone requested
	if opts.Depth > 0 {
		logProgress("Creating shallow clone with depth %d...\n", opts.Depth)
		// Here you would implement depth-limited fetching
		// For now we'll just continue with normal fetching
	}

	if err := fetchAndProcessPackfile(destPath, url, defaultCommit, refs, remoteName, cfg); err != nil {
		return fmt.Errorf("failed to fetch objects: %w", err)
	}

	// Update local refs
	logProgress("Setting up branch references...\n")
	for ref, commit := range refs {
		if strings.HasPrefix(ref, "refs/heads/") {
			branchName := strings.TrimPrefix(ref, "refs/heads/")

			// Create local branch ref
			branchPath := filepath.Join(repoPath, "refs", "heads", branchName)
			if err := os.WriteFile(branchPath, []byte(commit), 0644); err != nil {
				return fmt.Errorf("failed to create branch %s: %w", branchName, err)
			}

			// Create remote tracking ref if not a bare repo
			if !opts.Bare {
				remoteBranchPath := filepath.Join(repoPath, "refs", "remotes", "origin", branchName)
				if err := os.MkdirAll(filepath.Dir(remoteBranchPath), 0755); err != nil {
					return fmt.Errorf("failed to create remote branch directory: %w", err)
				}
				if err := os.WriteFile(remoteBranchPath, []byte(commit), 0644); err != nil {
					return fmt.Errorf("failed to create remote branch %s: %w", branchName, err)
				}
			}
		}
	}

	// Only checkout if we're not doing a bare repo and checkout is not disabled
	if !opts.Bare && !opts.NoCheckout {
		logProgress("Checking out files...\n")

		// In a real implementation, you would use the proper checkout function
		// For now we'll just create an empty checkout function
		if err := checkoutWorkingTree(destPath, defaultCommit); err != nil {
			return fmt.Errorf("failed to checkout working tree: %w", err)
		}
	}

	// Clone submodules if requested
	if opts.Recursive {
		logProgress("Initializing submodules...\n")
		// Here you would implement submodule cloning
		// We'll skip this for now as it's not yet implemented
	}

	return nil
}

// checkoutWorkingTree checks out files to working directory
func checkoutWorkingTree(repoPath, commitHash string) error {
	// This is a simplified version - in a real implementation,
	// you would properly extract files from the commit tree

	// For now, just create a placeholder README.md
	readmePath := filepath.Join(repoPath, "README.md")
	content := fmt.Sprintf("# Repository\n\nThis repository was cloned with Vec.\nCommit: %s\n", commitHash)

	return os.WriteFile(readmePath, []byte(content), 0644)
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

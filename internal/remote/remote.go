package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/config"
	"github.com/NahomAnteneh/vec/internal/merge"
	"github.com/NahomAnteneh/vec/utils"
)

// Remote protocol constants
const (
	DefaultRemoteName = "origin"
	ApiVersion        = "v1"
	DefaultTimeout    = 60 * time.Second
)

// Common error types
var (
	ErrRemoteNotFound       = errors.New("remote not found")
	ErrRemoteAlreadyExist   = errors.New("remote already exists")
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrNetworkError         = errors.New("network error occurred")
	ErrInvalidResponse      = errors.New("invalid response from server")
)

// RemoteInfo contains information about a remote repository
type RemoteInfo struct {
	Name          string
	URL           string
	DefaultBranch string
	Branches      []string
	LastFetched   int64
}

// AddRemote adds a new remote repository reference to the configuration
func AddRemote(repoRoot, name, url string) error {
	if name == "" || url == "" {
		return fmt.Errorf("remote name and URL cannot be empty")
	}

	// Load config
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check if remote already exists
	if _, exists := cfg.Remotes[name]; exists {
		return fmt.Errorf("%w: %s", ErrRemoteAlreadyExist, name)
	}

	// Add remote
	if err := cfg.AddRemote(name, url); err != nil {
		return fmt.Errorf("failed to add remote: %w", err)
	}

	// Save config
	if err := cfg.Write(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// RemoveRemote removes a remote repository reference from the configuration
func RemoveRemote(repoRoot, name string) error {
	// Load config
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check if remote exists
	if _, exists := cfg.Remotes[name]; !exists {
		return fmt.Errorf("%w: %s", ErrRemoteNotFound, name)
	}

	// Remove remote refs directory
	remoteRefsDir := filepath.Join(repoRoot, ".vec", "refs", "remotes", name)
	if utils.FileExists(remoteRefsDir) {
		if err := os.RemoveAll(remoteRefsDir); err != nil {
			return fmt.Errorf("failed to remove remote refs directory: %w", err)
		}
	}

	// Remove from config
	if err := cfg.RemoveRemote(name); err != nil {
		return fmt.Errorf("failed to remove remote from config: %w", err)
	}

	// Save config
	if err := cfg.Write(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// RenameRemote renames a remote repository reference in the configuration
func RenameRemote(repoRoot, oldName, newName string) error {
	// Load config
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check if old remote exists
	if _, exists := cfg.Remotes[oldName]; !exists {
		return fmt.Errorf("%w: %s", ErrRemoteNotFound, oldName)
	}

	// Check if new name already exists
	if _, exists := cfg.Remotes[newName]; exists {
		return fmt.Errorf("%w: %s", ErrRemoteAlreadyExist, newName)
	}

	// Rename remote in config
	if err := cfg.RenameRemote(oldName, newName); err != nil {
		return fmt.Errorf("failed to rename remote in config: %w", err)
	}

	// Rename refs directory if it exists
	oldRefsDir := filepath.Join(repoRoot, ".vec", "refs", "remotes", oldName)
	newRefsDir := filepath.Join(repoRoot, ".vec", "refs", "remotes", newName)
	if utils.FileExists(oldRefsDir) {
		// Ensure parent directory exists
		if err := utils.EnsureDirExists(filepath.Dir(newRefsDir)); err != nil {
			return fmt.Errorf("failed to create refs directory: %w", err)
		}
		if err := os.Rename(oldRefsDir, newRefsDir); err != nil {
			return fmt.Errorf("failed to rename remote refs directory: %w", err)
		}
	}

	// Save config
	if err := cfg.Write(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// SetRemoteURL updates the URL for a remote repository
func SetRemoteURL(repoRoot, name, url string) error {
	// Load config
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check if remote exists
	remote, exists := cfg.Remotes[name]
	if !exists {
		return fmt.Errorf("%w: %s", ErrRemoteNotFound, name)
	}

	// Update URL
	remote.URL = url
	cfg.Remotes[name] = remote

	// Save config
	if err := cfg.Write(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// SetRemoteAuth sets authentication information for a remote
func SetRemoteAuth(repoRoot, name, authToken string) error {
	// Load config
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check if remote exists
	if _, exists := cfg.Remotes[name]; !exists {
		return fmt.Errorf("%w: %s", ErrRemoteNotFound, name)
	}

	// Set auth token
	if err := cfg.SetRemoteAuth(name, authToken); err != nil {
		return fmt.Errorf("failed to set remote auth: %w", err)
	}

	// Save config
	if err := cfg.Write(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// ListRemotes retrieves information about all configured remotes
func ListRemotes(repoRoot string) (map[string]RemoteInfo, error) {
	remotes := make(map[string]RemoteInfo)

	// Load config to get remote URLs
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Get all configured remotes
	remoteURLs := cfg.Remotes
	if len(remoteURLs) == 0 {
		return remotes, nil
	}

	for name, remote := range remoteURLs {
		info := RemoteInfo{
			Name: name,
			URL:  remote.URL,
		}

		// Get tracked branches for this remote
		remoteBranchesDir := filepath.Join(repoRoot, ".vec", "refs", "remotes", name)
		if _, err := os.Stat(remoteBranchesDir); err == nil {
			branches, err := getBranchesForRemote(remoteBranchesDir)
			if err == nil && len(branches) > 0 {
				info.Branches = branches
				// Assume first branch is default if we have branches
				info.DefaultBranch = branches[0]
			}
		}

		// Get last fetched time if available
		fetchInfoPath := filepath.Join(repoRoot, ".vec", "FETCH_INFO", name)
		if fetchInfo, err := os.ReadFile(fetchInfoPath); err == nil {
			info.LastFetched = parseLastFetchedTime(string(fetchInfo))
		}

		remotes[name] = info
	}

	return remotes, nil
}

// Helper function to get branches for a remote
func getBranchesForRemote(remoteBranchesDir string) ([]string, error) {
	var branches []string

	err := filepath.Walk(remoteBranchesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			relPath, err := filepath.Rel(remoteBranchesDir, path)
			if err != nil {
				return err
			}
			branches = append(branches, relPath)
		}

		return nil
	})

	return branches, err
}

// Helper to parse last fetched time from fetch info file
func parseLastFetchedTime(fetchInfo string) int64 {
	lines := strings.Split(fetchInfo, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "timestamp:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				var timestamp int64
				fmt.Sscanf(parts[1], "%d", &timestamp)
				return timestamp
			}
		}
	}
	return 0
}

// GetRemoteInfo retrieves detailed information about a specific remote
func GetRemoteInfo(repoRoot, name string) (*RemoteInfo, error) {
	// Load config
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Check if remote exists
	remote, exists := cfg.Remotes[name]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrRemoteNotFound, name)
	}

	// Get list of remote branches
	branches, _ := listRemoteBranches(repoRoot, name)

	// Determine default branch
	defaultBranch := "main" // Default
	if len(branches) > 0 {
		defaultBranch = branches[0]
	}

	return &RemoteInfo{
		Name:          name,
		URL:           remote.URL,
		DefaultBranch: defaultBranch,
		Branches:      branches,
	}, nil
}

// ListRemoteBranches lists all branches from a specific remote
func listRemoteBranches(repoRoot, remoteName string) ([]string, error) {
	branches := []string{}

	remoteBranchesDir := filepath.Join(repoRoot, ".vec", "refs", "remotes", remoteName)
	if !utils.FileExists(remoteBranchesDir) {
		return branches, nil
	}

	err := filepath.Walk(remoteBranchesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			relPath, err := filepath.Rel(remoteBranchesDir, path)
			if err != nil {
				return err
			}
			branches = append(branches, relPath)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list remote branches: %w", err)
	}

	return branches, nil
}

// makeRemoteRequest performs an HTTP request to a remote repository
func makeRemoteRequest(remoteURL, endpoint string, method string, data interface{}, cfg *config.Config, remoteName string) (*http.Response, error) {
	client := &http.Client{
		Timeout: DefaultTimeout,
	}

	// Prepare URL - remoteURL already includes /api/username/repo
	apiURL := fmt.Sprintf("%s/%s", remoteURL, endpoint)

	var body io.Reader
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request data: %w", err)
		}
		body = bytes.NewBuffer(jsonData)
	}

	// Create request
	req, err := http.NewRequest(method, apiURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Vec-Client/0.1")

	// Add authentication if available
	if err := ApplyAuthHeaders(req, remoteName, cfg); err != nil {
		return nil, fmt.Errorf("failed to apply auth headers: %w", err)
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetworkError, err)
	}

	// Check for authentication errors
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return nil, ErrAuthenticationFailed
	}

	return resp, nil
}

// prune removes stale remote references
func prune(repoRoot, remoteName string) error {
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

	// Fetch remote refs directly from server
	remoteRefs, err := fetchRemoteRefs(remoteURL, remoteName, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch remote refs: %w", err)
	}

	// Get local tracking branches for this remote
	remoteBranchesDir := filepath.Join(repoRoot, ".vec", "refs", "remotes", remoteName)
	if !utils.FileExists(remoteBranchesDir) {
		return nil // No local tracking branches
	}

	// Walk through all tracking refs and remove ones that don't exist on remote
	return filepath.Walk(remoteBranchesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			relPath, err := filepath.Rel(remoteBranchesDir, path)
			if err != nil {
				return err
			}

			// Check if the branch exists on remote
			refKey := "refs/heads/" + relPath
			if _, exists := remoteRefs[refKey]; !exists {
				// Remove stale reference
				if err := os.Remove(path); err != nil {
					return fmt.Errorf("failed to remove stale reference %s: %w", relPath, err)
				}
			}
		}

		return nil
	})
}

// MergeRemoteBranch merges a remote branch into the current local branch
func MergeRemoteBranch(repoRoot, remoteName, remoteBranch string, interactive bool) error {
	// Validate repository
	vecDir := filepath.Join(repoRoot, ".vec")
	if _, err := os.Stat(vecDir); os.IsNotExist(err) {
		return fmt.Errorf("not a vec repository: %s", repoRoot)
	}

	// Load config to get remote info
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Verify the remote exists
	_, err = cfg.GetRemoteURL(remoteName)
	if err != nil {
		return fmt.Errorf("remote '%s' not found: %w", remoteName, err)
	}

	// Check if the remote branch exists
	remoteBranchPath := filepath.Join(vecDir, "refs", "remotes", remoteName, remoteBranch)
	if _, err := os.Stat(remoteBranchPath); os.IsNotExist(err) {
		return fmt.Errorf("remote branch '%s/%s' not found, try fetching first", remoteName, remoteBranch)
	}

	// Get the current branch
	currentBranch, err := merge.GetCurrentBranch(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to determine current branch: %w", err)
	}

	// Create a temporary branch name for the remote branch
	tempBranchName := fmt.Sprintf("MERGE_HEAD_%s_%s", remoteName, remoteBranch)

	// Read the remote branch commit hash
	remoteCommitBytes, err := os.ReadFile(remoteBranchPath)
	if err != nil {
		return fmt.Errorf("failed to read remote branch commit: %w", err)
	}

	// Create a temporary ref for the merge
	tempRefPath := filepath.Join(vecDir, "refs", "heads", tempBranchName)
	if err := os.MkdirAll(filepath.Dir(tempRefPath), 0755); err != nil {
		return fmt.Errorf("failed to create temporary ref directory: %w", err)
	}
	if err := os.WriteFile(tempRefPath, remoteCommitBytes, 0644); err != nil {
		return fmt.Errorf("failed to create temporary ref: %w", err)
	}

	// Clean up the temporary branch when we're done
	defer os.Remove(tempRefPath)

	// Set up merge configuration
	mergeConfig := &merge.MergeConfig{
		Strategy:    merge.MergeStrategyRecursive,
		Interactive: interactive,
	}

	// Perform the merge
	fmt.Printf("Merging remote branch '%s/%s' into local branch '%s'\n",
		remoteName, remoteBranch, currentBranch)

	hasConflicts, err := merge.Merge(repoRoot, tempBranchName, mergeConfig)
	if err != nil {
		if strings.Contains(err.Error(), "already up-to-date") {
			fmt.Printf("Branch '%s' is already up-to-date with '%s/%s'\n",
				currentBranch, remoteName, remoteBranch)
			return nil
		}
		return fmt.Errorf("merge failed: %w", err)
	}

	if hasConflicts {
		fmt.Printf("Merge conflicts between '%s' and '%s/%s'. Please resolve and commit.\n",
			currentBranch, remoteName, remoteBranch)
		return nil
	}

	fmt.Printf("Successfully merged '%s/%s' into '%s'\n",
		remoteName, remoteBranch, currentBranch)
	return nil
}

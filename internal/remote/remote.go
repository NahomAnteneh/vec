package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/config"
	"github.com/NahomAnteneh/vec/internal/merge"
	vechttp "github.com/NahomAnteneh/vec/internal/remote/http"
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

	// Remove credentials for this remote
	ClearCredentials(name)

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

// SetRemoteAuth sets authentication credentials for a remote
func SetRemoteAuth(repoRoot, name, username, password string) error {
	// Load config
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check if remote exists
	if _, exists := cfg.Remotes[name]; !exists {
		return fmt.Errorf("%w: %s", ErrRemoteNotFound, name)
	}

	// Store credentials
	if err := StoreCredentials(name, username, password); err != nil {
		return fmt.Errorf("failed to store credentials: %w", err)
	}

	return nil
}

// GetRemoteURL retrieves the URL for a given remote name
func GetRemoteURL(repoRoot, remoteName string) (string, error) {
	// Load config
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	// Get remote URL
	remoteURL, err := cfg.GetRemoteURL(remoteName)
	if err != nil {
		return "", err
	}

	if remoteURL == "" {
		return "", fmt.Errorf("remote '%s' not found or has no URL configured", remoteName)
	}

	return remoteURL, nil
}

// ListRemotes lists all configured remotes for the repository
func ListRemotes(repoRoot string) (map[string]RemoteInfo, error) {
	// Load config
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	result := make(map[string]RemoteInfo)
	for name, remote := range cfg.Remotes {
		branches, _ := listRemoteBranches(repoRoot, name)
		
		info := RemoteInfo{
			Name:     name,
			URL:      remote.URL,
			Branches: branches,
		}
		
		// Try to read last fetched info
		fetchInfoPath := filepath.Join(repoRoot, ".vec", "refs", "remotes", name, "FETCH_HEAD")
		if utils.FileExists(fetchInfoPath) {
			fetchInfo, err := os.ReadFile(fetchInfoPath)
			if err == nil {
				info.LastFetched = parseLastFetchedTime(string(fetchInfo))
			}
		}
		
		result[name] = info
	}

	return result, nil
}

// getBranchesForRemote lists all branches for a remote
func getBranchesForRemote(remoteBranchesDir string) ([]string, error) {
	if !utils.FileExists(remoteBranchesDir) {
		return []string{}, nil
	}

	entries, err := os.ReadDir(remoteBranchesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read remote branches directory: %w", err)
	}

	branches := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && entry.Name() != "HEAD" && entry.Name() != "FETCH_HEAD" {
			branches = append(branches, entry.Name())
		}
	}

	return branches, nil
}

// parseLastFetchedTime extracts timestamp from fetch info
func parseLastFetchedTime(fetchInfo string) int64 {
	// Look for timestamp in fetch info
	index := strings.Index(fetchInfo, "# last_fetched=")
	if index == -1 {
		return 0
	}
	
	timestampStr := strings.TrimSpace(fetchInfo[index+len("# last_fetched="):])
	timestamp, _ := strconv.ParseInt(timestampStr, 10, 64)
	return timestamp
}

// GetRemoteInfo gets detailed information about a remote
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

	branches, _ := listRemoteBranches(repoRoot, name)
	
	info := &RemoteInfo{
		Name:     name,
		URL:      remote.URL,
		Branches: branches,
	}
	
	// Try to read last fetched info
	fetchInfoPath := filepath.Join(repoRoot, ".vec", "refs", "remotes", name, "FETCH_HEAD")
	if utils.FileExists(fetchInfoPath) {
		fetchInfo, err := os.ReadFile(fetchInfoPath)
		if err == nil {
			info.LastFetched = parseLastFetchedTime(string(fetchInfo))
		}
	}

	return info, nil
}

// listRemoteBranches lists all branches for a remote
func listRemoteBranches(repoRoot, remoteName string) ([]string, error) {
	remoteBranchesDir := filepath.Join(repoRoot, ".vec", "refs", "remotes", remoteName)
	return getBranchesForRemote(remoteBranchesDir)
}

// makeRemoteRequest sends an HTTP request to the remote repository
func makeRemoteRequest(remoteURL, endpoint string, method string, data interface{}, cfg *config.Config, remoteName string) (*http.Response, error) {
	client := vechttp.NewClient(remoteURL, remoteName, cfg)
	client.SetTimeout(DefaultTimeout)
	
	var respData []byte
	var err error
	
	if method == "GET" {
		respData, err = client.Get(endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to make GET request: %w", err)
		}
	} else if method == "POST" {
		respData, err = client.Post(endpoint, data)
		if err != nil {
			return nil, fmt.Errorf("failed to make POST request: %w", err)
		}
	} else {
		return nil, fmt.Errorf("unsupported HTTP method: %s", method)
	}
	
	// Create a response object to return
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(respData))),
	}
	
	return resp, nil
}

// prune removes obsolete remote-tracking branches
func prune(repoRoot, remoteName string) error {
	// Load config
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check if remote exists
	if _, exists := cfg.Remotes[remoteName]; !exists {
		return fmt.Errorf("%w: %s", ErrRemoteNotFound, remoteName)
	}

	// Get current remote branches
	remoteURL, err := GetRemoteURL(repoRoot, remoteName)
	if err != nil {
		return fmt.Errorf("failed to get remote URL: %w", err)
	}

	client := vechttp.NewClient(remoteURL, remoteName, cfg)
	remoteRefs, err := client.GetRefs()
	if err != nil {
		return fmt.Errorf("failed to get remote refs: %w", err)
	}

	// Get local remote-tracking branches
	localRemoteBranches, err := listRemoteBranches(repoRoot, remoteName)
	if err != nil {
		return fmt.Errorf("failed to list local remote branches: %w", err)
	}

	// Find obsolete branches
	obsoleteBranches := make([]string, 0)
	for _, branch := range localRemoteBranches {
		refName := fmt.Sprintf("refs/heads/%s", branch)
		if _, exists := remoteRefs[refName]; !exists {
			obsoleteBranches = append(obsoleteBranches, branch)
		}
	}

	// Remove obsolete branches
	for _, branch := range obsoleteBranches {
		branchPath := filepath.Join(repoRoot, ".vec", "refs", "remotes", remoteName, branch)
		if err := os.Remove(branchPath); err != nil {
			return fmt.Errorf("failed to remove obsolete branch %s: %w", branch, err)
		}
	}

	return nil
}

// MergeRemoteBranch merges a remote branch into the current branch
func MergeRemoteBranch(repoRoot, remoteName, remoteBranch string, interactive bool) error {
	repo := core.NewRepository(repoRoot)
	return MergeRemoteBranchRepo(repo, remoteName, remoteBranch, interactive)
}

// MergeRemoteBranchRepo merges a remote branch into the current branch using Repository context
func MergeRemoteBranchRepo(repo *core.Repository, remoteName, remoteBranch string, interactive bool) error {
	// Get current branch
	currentBranch, err := repo.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if currentBranch == "(HEAD detached)" {
		return fmt.Errorf("cannot merge when in detached HEAD state")
	}

	// Get remote branch commit
	remoteBranchPath := filepath.Join(repo.VecDir, "refs", "remotes", remoteName, remoteBranch)
	if !utils.FileExists(remoteBranchPath) {
		return fmt.Errorf("remote branch '%s/%s' not found", remoteName, remoteBranch)
	}

	remoteBranchCommit, err := utils.ReadRef(repo.Root, filepath.Join("refs", "remotes", remoteName, remoteBranch))
	if err != nil {
		return fmt.Errorf("failed to read remote branch commit: %w", err)
	}

	// Get current branch commit
	currentCommit, err := repo.GetHeadCommit()
	if err != nil {
		return fmt.Errorf("failed to get current commit: %w", err)
	}

	// Check if merge is needed
	if currentCommit == remoteBranchCommit {
		return fmt.Errorf("already up-to-date with '%s/%s'", remoteName, remoteBranch)
	}

	// Perform the merge
	mergeResult, err := merge.Merge(
		repo.Root,
		currentCommit,
		remoteBranchCommit,
		fmt.Sprintf("Merge remote branch '%s/%s' into %s", remoteName, remoteBranch, currentBranch),
		interactive,
	)
	if err != nil {
		return fmt.Errorf("merge failed: %w", err)
	}

	if mergeResult.FastForward {
		fmt.Printf("Fast-forward merge, updated %s to %s\n", currentBranch, mergeResult.MergeCommit[:7])
	} else if mergeResult.Success {
		fmt.Printf("Merge completed successfully, created commit %s\n", mergeResult.MergeCommit[:7])
	} else {
		return fmt.Errorf("merge resulted in conflicts, please resolve them manually")
	}

	return nil
}

// ParseLastFetchedTime is an exported version of parseLastFetchedTime
func ParseLastFetchedTime(fetchInfo string) int64 {
	return parseLastFetchedTime(fetchInfo)
}

// Prune is an exported version of prune
func Prune(repoRoot, remoteName string) error {
	return prune(repoRoot, remoteName)
}

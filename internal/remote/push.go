// internal/remote/push.go
package remote

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/config"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/utils"
)

// PushResult contains the result of a push operation
type PushResult struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Errors  []string `json:"errors,omitempty"`
}

// PushOptions configures the behavior of the push operation
type PushOptions struct {
	// Force allows non-fast-forward updates
	Force bool
	// Verbose provides detailed progress information
	Verbose bool
	// Timeout specifies the maximum time for the entire push operation
	Timeout time.Duration
	// IncludeTags determines whether to also push tags pointing to the pushed commits
	IncludeTags bool
}

// DefaultPushOptions returns the default options for push operations
func DefaultPushOptions() PushOptions {
	return PushOptions{
		Force:       false,
		Verbose:     false,
		Timeout:     5 * time.Minute,
		IncludeTags: false,
	}
}

// Push sends local commits to a remote repository
func Push(repoRoot, remoteName, branchName string, force bool) error {
	options := DefaultPushOptions()
	options.Force = force
	return PushWithOptions(repoRoot, remoteName, branchName, options)
}

// PushWithOptions sends local commits to a remote repository with the specified options
func PushWithOptions(repoRoot, remoteName, branchName string, options PushOptions) error {
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

	// Determine local branch
	if branchName == "" {
		// Get current branch
		currentBranch, err := getCurrentBranch(repoRoot)
		if err != nil {
			return fmt.Errorf("failed to get current branch: %w", err)
		}
		branchName = currentBranch
	}

	if options.Verbose {
		fmt.Printf("Pushing branch %s to remote %s...\n", branchName, remoteName)
	}

	// Get local branch commit
	localCommitHash, err := getLocalBranchCommit(repoRoot, branchName)
	if err != nil {
		return fmt.Errorf("failed to get local branch commit: %w", err)
	}

	// Get remote branch commit, if it exists
	remoteCommitHash, err := getRemoteBranchCommit(remoteURL, remoteName, branchName, cfg)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return fmt.Errorf("failed to get remote branch commit: %w", err)
	}

	// Check if we need to push (no change if remote is already up to date)
	if localCommitHash == remoteCommitHash {
		if options.Verbose {
			fmt.Printf("Branch %s is already up to date on remote %s\n", branchName, remoteName)
		}
		return nil
	}

	// If not forcing, verify this is a fast-forward push
	if !options.Force && remoteCommitHash != "" {
		isFastForward, err := isFastForwardUpdate(repoRoot, localCommitHash, remoteCommitHash)
		if err != nil {
			return fmt.Errorf("failed to check if update is fast-forward: %w", err)
		}
		if !isFastForward {
			return fmt.Errorf("non-fast-forward update. Use --force to override")
		}
	}

	// Find all objects that need to be sent
	if options.Verbose {
		fmt.Println("Determining objects to send...")
	}

	objectsToSend, err := getObjectsToSend(repoRoot, localCommitHash, remoteCommitHash)
	if err != nil {
		return fmt.Errorf("failed to determine objects to send: %w", err)
	}

	if len(objectsToSend) == 0 {
		if options.Verbose {
			fmt.Println("No objects to send. Remote is up to date.")
		}
		return nil
	}

	// Include tags if requested
	if options.IncludeTags {
		tagObjects, err := getTagsForPush(repoRoot, objectsToSend)
		if err != nil {
			return fmt.Errorf("failed to get tag objects: %w", err)
		}
		objectsToSend = append(objectsToSend, tagObjects...)
	}

	// Create packfile
	if options.Verbose {
		fmt.Printf("Creating packfile with %d objects...\n", len(objectsToSend))
	}

	packfile, err := createPackfile(repoRoot, objectsToSend)
	if err != nil {
		return fmt.Errorf("failed to create packfile: %w", err)
	}

	// Send packfile and update refs
	pushData := map[string]interface{}{
		"branch":    branchName,
		"oldCommit": remoteCommitHash,
		"newCommit": localCommitHash,
		"force":     options.Force,
	}

	// Perform push with timeout
	client := &http.Client{
		Timeout: options.Timeout,
	}

	if options.Verbose {
		fmt.Println("Sending packfile to remote...")
	}

	result, err := performPushWithClient(remoteURL, remoteName, pushData, packfile, cfg, client)
	if err != nil {
		return fmt.Errorf("push failed: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("push rejected: %s", result.Message)
	}

	if options.Verbose {
		fmt.Printf("Successfully pushed branch %s to %s\n", branchName, remoteName)
	} else {
		fmt.Printf("Successfully pushed branch %s to %s\n", branchName, remoteName)
	}
	return nil
}

// getCurrentBranch gets the name of the current branch
func getCurrentBranch(repoRoot string) (string, error) {
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

// getLocalBranchCommit gets the commit hash of a local branch
func getLocalBranchCommit(repoRoot, branchName string) (string, error) {
	branchFile := filepath.Join(repoRoot, ".vec", "refs", "heads", branchName)
	if !utils.FileExists(branchFile) {
		return "", fmt.Errorf("branch %s not found", branchName)
	}

	content, err := os.ReadFile(branchFile)
	if err != nil {
		return "", fmt.Errorf("failed to read branch file: %w", err)
	}

	return strings.TrimSpace(string(content)), nil
}

// getRemoteBranchCommit gets the commit hash of a remote branch
func getRemoteBranchCommit(remoteURL, remoteName, branchName string, cfg *config.Config) (string, error) {
	endpoint := fmt.Sprintf("refs/heads/%s", branchName)
	resp, err := makeRemoteRequest(remoteURL, endpoint, "GET", nil, cfg, remoteName)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("branch %s not found on remote", branchName)
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to get remote branch with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Commit string `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Commit, nil
}

// isFastForwardUpdate checks if updating from oldCommit to newCommit is a fast-forward update
func isFastForwardUpdate(repoRoot, newCommit, oldCommit string) (bool, error) {
	// Check if oldCommit is an ancestor of newCommit by walking the commit history

	// Get the commit object
	commit, err := objects.GetCommit(repoRoot, newCommit)
	if err != nil {
		return false, fmt.Errorf("failed to get commit %s: %w", newCommit, err)
	}

	// Check if this is the old commit we're looking for
	if newCommit == oldCommit {
		return true, nil
	}

	// Recursively check each parent
	for _, parentHash := range commit.Parents {
		isAncestor, err := isFastForwardUpdate(repoRoot, parentHash, oldCommit)
		if err != nil {
			return false, err
		}
		if isAncestor {
			return true, nil
		}
	}

	return false, nil
}

// getTagsForPush finds tag objects that should be included in the push
func getTagsForPush(repoRoot string, objectHashes []string) ([]string, error) {
	// Get tags that point to the objects being pushed
	tagsDir := filepath.Join(repoRoot, ".vec", "refs", "tags")
	if !utils.FileExists(tagsDir) {
		return nil, nil
	}

	tagObjects := []string{}
	err := filepath.Walk(tagsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			tagContents, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			tagHash := strings.TrimSpace(string(tagContents))

			// Check if this tag points to any object we're sending
			for _, objHash := range objectHashes {
				if tagHash == objHash {
					// Add tag object to the list
					tagObjects = append(tagObjects, tagHash)
					break
				}
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan tags: %w", err)
	}

	return tagObjects, nil
}

// getObjectsToSend finds all objects that need to be sent to the remote
func getObjectsToSend(repoRoot, localCommit, remoteCommit string) ([]string, error) {
	// Start with just the local commit
	objectsToSend := []string{localCommit}

	// Get the commit object
	commit, err := objects.GetCommit(repoRoot, localCommit)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit object %s: %w", localCommit, err)
	}

	// Add the tree
	objectsToSend = append(objectsToSend, commit.Tree)

	// Add tree contents recursively
	treeObjects, err := getTreeObjectsRecursive(repoRoot, commit.Tree)
	if err != nil {
		return nil, fmt.Errorf("failed to get tree objects: %w", err)
	}
	objectsToSend = append(objectsToSend, treeObjects...)

	// Process parent commits if this isn't the remote commit
	if localCommit != remoteCommit {
		for _, parentHash := range commit.Parents {
			// Skip if this is the remote commit
			if parentHash == remoteCommit {
				continue
			}

			// Recursively get objects from parent commits
			parentObjects, err := getObjectsToSend(repoRoot, parentHash, remoteCommit)
			if err != nil {
				return nil, err
			}

			// Add unique objects
			for _, obj := range parentObjects {
				// Check if object is already in our list
				found := false
				for _, existing := range objectsToSend {
					if existing == obj {
						found = true
						break
					}
				}

				if !found {
					objectsToSend = append(objectsToSend, obj)
				}
			}
		}
	}

	return objectsToSend, nil
}

// getTreeObjectsRecursive traverses a tree object and returns all objects within it
func getTreeObjectsRecursive(repoRoot, treeHash string) ([]string, error) {
	objectsList := []string{treeHash}

	// Get the tree object
	tree, err := objects.GetTree(repoRoot, treeHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get tree object %s: %w", treeHash, err)
	}

	// Process each entry in the tree
	for _, entry := range tree.Entries {
		objectsList = append(objectsList, entry.Hash)

		// If entry is a subtree, process it recursively
		if entry.Type == "tree" {
			subObjects, err := getTreeObjectsRecursive(repoRoot, entry.Hash)
			if err != nil {
				return nil, err
			}
			objectsList = append(objectsList, subObjects...)
		}
	}

	return objectsList, nil
}

// createPackfile creates a packfile containing the specified objects
func createPackfile(repoRoot string, objectHashes []string) ([]byte, error) {
	if len(objectHashes) == 0 {
		return nil, fmt.Errorf("no objects to pack")
	}

	// Create a temporary file to store the packfile
	tempDir := os.TempDir()
	tempFile, err := os.CreateTemp(tempDir, "vec-packfile-*.pack")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary packfile: %w", err)
	}
	tempFilePath := tempFile.Name()
	tempFile.Close() // Close immediately as CreatePackfile will open it

	// Clean up the temporary file when done
	defer os.Remove(tempFilePath)

	// Create index file alongside the packfile
	createIndex := true

	// Create the packfile using the new format with compression and delta encoding
	err = objects.CreatePackfile(repoRoot, objectHashes, tempFilePath, createIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to create packfile: %w", err)
	}

	// Read the packfile contents
	packfileData, err := os.ReadFile(tempFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read packfile: %w", err)
	}

	// If we created an index, clean it up too
	if createIndex {
		defer os.Remove(tempFilePath + ".idx")
	}

	return packfileData, nil
}

// performPushWithClient sends the packfile and updates refs on the remote using the provided HTTP client
func performPushWithClient(remoteURL, remoteName string, pushData map[string]interface{}, packfile []byte, cfg *config.Config, client *http.Client) (*PushResult, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	endpoint := "push"

	// Combine pushData and packfile
	pushData["packfile"] = string(packfile)

	resp, err := makeRemoteRequest(remoteURL, endpoint, "POST", pushData, cfg, remoteName)
	if err != nil {
		return nil, fmt.Errorf("push request failed: %w", err)
	}
	defer resp.Body.Close()

	var result PushResult
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		result.Success = false
		result.Message = fmt.Sprintf("Server returned status %d: %s", resp.StatusCode, string(bodyBytes))
		return &result, nil
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode push response: %w", err)
	}

	return &result, nil
}

// performPush sends the packfile and updates refs on the remote
func performPush(remoteURL, remoteName string, pushData map[string]interface{}, packfile []byte, cfg *config.Config) (*PushResult, error) {
	return performPushWithClient(remoteURL, remoteName, pushData, packfile, cfg, nil)
}

// ApplyAuthHeaders adds authentication headers to the request
func ApplyAuthHeaders(req *http.Request, remoteName string, cfg *config.Config) error {
	// Get authentication token
	auth, err := cfg.GetRemoteAuth(remoteName)
	if err != nil {
		// Auth not set is not an error, just continue without auth
		return nil
	}

	if auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", auth))
	}

	// Add any custom headers
	headers, err := cfg.GetRemoteHeaders(remoteName)
	if err == nil && headers != nil {
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}

	return nil
}

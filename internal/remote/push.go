// internal/remote/push.go
package remote

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/config"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/packfile"
	vechttp "github.com/NahomAnteneh/vec/internal/remote/http"
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
	// DryRun simulates a push without actually sending any data
	DryRun bool
	// Progress indicates whether to show progress information
	Progress bool
}

// DefaultPushOptions returns the default options for push operations
func DefaultPushOptions() PushOptions {
	return PushOptions{
		Force:    false,
		Verbose:  false,
		Timeout:  5 * time.Minute,
		DryRun:   false,
		Progress: true,
	}
}

// Push sends local commits to a remote repository
func Push(repoRoot, remoteName, branchName string, force bool) error {
	options := DefaultPushOptions()
	options.Force = force
	return PushWithOptions(repoRoot, remoteName, branchName, options)
}

// PushRepo sends local commits to a remote repository using Repository context
func PushRepo(repo *core.Repository, remoteName, branchName string, force bool) error {
	options := DefaultPushOptions()
	options.Force = force
	return PushWithOptionsRepo(repo, remoteName, branchName, options)
}

// PushWithOptionsRepo sends local commits to a remote repository with the specified options using Repository context
func PushWithOptionsRepo(repo *core.Repository, remoteName, branchName string, options PushOptions) error {
	// Load config
	cfg, err := config.LoadConfigRepo(repo)
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
		currentBranch, err := repo.GetCurrentBranch()
		if err != nil {
			return fmt.Errorf("failed to get current branch: %w", err)
		}
		branchName = currentBranch
	}

	if options.Verbose {
		fmt.Printf("Pushing branch %s to remote %s...\n", branchName, remoteName)
	}

	// Get local branch commit
	localCommitHash, err := getLocalBranchCommitRepo(repo, branchName)
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
		isFastForward, err := isFastForwardUpdateRepo(repo, localCommitHash, remoteCommitHash)
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

	objectsToSend, err := getObjectsToSendRepo(repo, localCommitHash, remoteCommitHash)
	if err != nil {
		return fmt.Errorf("failed to determine objects to send: %w", err)
	}

	if len(objectsToSend) == 0 {
		if options.Verbose {
			fmt.Println("No objects to send. Remote is up to date.")
		}
		return nil
	}

	// Early return for dry run mode
	if options.DryRun {
		if options.Verbose {
			fmt.Printf("Dry run: Would push %d objects to update branch %s on remote %s\n",
				len(objectsToSend), branchName, remoteName)
			fmt.Printf("Dry run: Would update remote ref from %s to %s\n",
				formatCommitHash(remoteCommitHash), formatCommitHash(localCommitHash))
		}
		return nil
	}

	// Create packfile
	if options.Progress || options.Verbose {
		fmt.Printf("Creating packfile with %d objects...\n", len(objectsToSend))
	}

	packfile, err := createPackfileRepo(repo, objectsToSend)
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

	if options.Progress || options.Verbose {
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
		if len(result.Message) > 0 {
			fmt.Printf("Server message: %s\n", result.Message)
		}
	} else if options.Progress {
		fmt.Printf("Successfully pushed branch %s to %s\n", branchName, remoteName)
	}

	return nil
}

// Legacy function for backward compatibility
func PushWithOptions(repoRoot, remoteName, branchName string, options PushOptions) error {
	repo := core.NewRepository(repoRoot)
	return PushWithOptionsRepo(repo, remoteName, branchName, options)
}

// formatCommitHash formats a commit hash for display
func formatCommitHash(hash string) string {
	if hash == "" {
		return "new ref"
	}
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
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

// getLocalBranchCommitRepo gets the commit hash of a local branch using Repository context
func getLocalBranchCommitRepo(repo *core.Repository, branchName string) (string, error) {
	branchFile := filepath.Join(repo.VecDir, "refs", "heads", branchName)
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
	endpoint := fmt.Sprintf("branches/%s", branchName)
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
	err = packfile.CreatePackfileFromHashes(repoRoot, objectHashes, tempFilePath, createIndex)
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

// convertPushResult converts a vechttp.PushResult to a local PushResult
func convertPushResult(result *vechttp.PushResult) *PushResult {
	if result == nil {
		return nil
	}

	return &PushResult{
		Success: result.Success,
		Message: result.Message,
		Errors:  result.Errors,
	}
}

// performPushWithClient sends the packfile and updates refs on the remote using the provided HTTP client
func performPushWithClient(remoteURL, remoteName string, pushData map[string]interface{}, packfile []byte, cfg *config.Config, client *http.Client) (*PushResult, error) {
	// Ignore the client parameter and use our centralized client instead
	result, err := vechttp.PerformPush(remoteURL, remoteName, pushData, packfile, cfg)
	if err != nil {
		return nil, err
	}

	return convertPushResult(result), nil
}

// performPush sends the packfile and updates refs on the remote
func performPush(remoteURL, remoteName string, pushData map[string]interface{}, packfile []byte, cfg *config.Config) (*PushResult, error) {
	result, err := vechttp.PerformPush(remoteURL, remoteName, pushData, packfile, cfg)
	if err != nil {
		return nil, err
	}

	return convertPushResult(result), nil
}

// isFastForwardUpdateRepo checks if push is a fast-forward update using Repository context
func isFastForwardUpdateRepo(repo *core.Repository, localCommitHash, remoteCommitHash string) (bool, error) {
	// Check if remote commit is an ancestor of local commit
	isAncestor, err := isCommitAncestorRepo(repo, remoteCommitHash, localCommitHash)
	if err != nil {
		return false, fmt.Errorf("failed to check if remote commit is ancestor: %w", err)
	}

	return isAncestor, nil
}

// isCommitAncestorRepo checks if one commit is an ancestor of another using Repository context
func isCommitAncestorRepo(repo *core.Repository, ancestorHash, descendantHash string) (bool, error) {
	// Follow the commit chain to see if ancestorHash appears
	visited := make(map[string]bool)
	queue := []string{descendantHash}

	for len(queue) > 0 {
		hash := queue[0]
		queue = queue[1:]

		if hash == ancestorHash {
			return true, nil
		}

		if visited[hash] {
			continue
		}
		visited[hash] = true

		commit, err := objects.GetCommitRepo(repo, hash)
		if err != nil {
			return false, fmt.Errorf("failed to get commit %s: %w", hash, err)
		}

		queue = append(queue, commit.Parents...)
	}

	return false, nil
}

// getObjectsToSendRepo gets objects that need to be sent to the remote using Repository context
func getObjectsToSendRepo(repo *core.Repository, localCommitHash, remoteCommitHash string) ([]string, error) {
	// Get all objects reachable from local commit
	localObjects, err := findReachableObjectsRepo(repo, localCommitHash)
	if err != nil {
		return nil, fmt.Errorf("failed to find local objects: %w", err)
	}

	// If remote has no commit yet, send all objects
	if remoteCommitHash == "" {
		return localObjects, nil
	}

	// Get all objects reachable from remote commit
	remoteObjects, err := findReachableObjectsRepo(repo, remoteCommitHash)
	if err != nil {
		return nil, fmt.Errorf("failed to find remote objects: %w", err)
	}

	// Filter out objects already on remote
	var objectsToSend []string
	for _, obj := range localObjects {
		found := false
		for _, remoteObj := range remoteObjects {
			if obj == remoteObj {
				found = true
				break
			}
		}
		if !found {
			objectsToSend = append(objectsToSend, obj)
		}
	}

	return objectsToSend, nil
}

// findReachableObjectsRepo finds all objects reachable from a commit using Repository context
func findReachableObjectsRepo(repo *core.Repository, commitHash string) ([]string, error) {
	if commitHash == "" {
		return []string{}, nil
	}

	objectsMap := make(map[string]bool)
	visited := make(map[string]bool)
	queue := []string{commitHash}

	for len(queue) > 0 {
		hash := queue[0]
		queue = queue[1:]

		if visited[hash] {
			continue
		}
		visited[hash] = true
		objectsMap[hash] = true

		// Get object type by reading the object header
		objPath := filepath.Join(repo.VecDir, "objects", hash[:2], hash[2:])
		if !utils.FileExists(objPath) {
			return nil, fmt.Errorf("object %s not found", hash)
		}

		// Read and decompress the object file
		file, err := os.Open(objPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open object file %s: %w", hash, err)
		}
		defer file.Close()

		// Create zlib reader
		zr, err := zlib.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("failed to create zlib reader for %s: %w", hash, err)
		}
		defer zr.Close()

		// Read decompressed content
		content, err := io.ReadAll(zr)
		if err != nil {
			return nil, fmt.Errorf("failed to read decompressed content for %s: %w", hash, err)
		}

		// Parse the object header (format: "type size\0content")
		nullIndex := bytes.IndexByte(content, 0)
		if nullIndex == -1 {
			return nil, fmt.Errorf("invalid object format for %s", hash)
		}

		header := string(content[:nullIndex])
		parts := strings.Split(header, " ")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid object header format for %s", hash)
		}

		objType := parts[0] // "commit", "tree", or "blob"

		switch objType {
		case "commit":
			commit, err := objects.GetCommit(repo.Root, hash)
			if err != nil {
				return nil, fmt.Errorf("failed to get commit: %w", err)
			}
			objectsMap[commit.Tree] = true
			queue = append(queue, commit.Tree)
			queue = append(queue, commit.Parents...)

		case "tree":
			tree, err := objects.GetTree(repo.Root, hash)
			if err != nil {
				return nil, fmt.Errorf("failed to get tree: %w", err)
			}
			for _, entry := range tree.Entries {
				objectsMap[entry.Hash] = true
				queue = append(queue, entry.Hash)
			}
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(objectsMap))
	for obj := range objectsMap {
		result = append(result, obj)
	}

	return result, nil
}

// createPackfileRepo creates a packfile containing the given objects using Repository context
func createPackfileRepo(repo *core.Repository, objectHashes []string) ([]byte, error) {
	// Create temporary packfile
	tempFile, err := os.CreateTemp("", "vec-packfile-*.pack")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	// Create packfile
	err = packfile.CreatePackfileFromHashesRepo(repo, objectHashes, tempPath, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create packfile: %w", err)
	}

	// Read packfile content
	content, err := os.ReadFile(tempPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read packfile: %w", err)
	}

	return content, nil
}

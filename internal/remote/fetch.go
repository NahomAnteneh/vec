// internal/remote/fetch.go
package remote

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"

	"compress/zlib"

	"github.com/NahomAnteneh/vec/internal/config"
	"github.com/NahomAnteneh/vec/internal/objects"
	vechttp "github.com/NahomAnteneh/vec/internal/remote/http"
	"github.com/NahomAnteneh/vec/utils"
)

// FetchOptions contains options for the fetch operation
type FetchOptions struct {
	// General options
	Quiet     bool   // Suppress output
	Verbose   bool   // Be verbose
	Force     bool   // Force update of local branches
	Depth     int    // Create a shallow fetch with limited history
	FetchTags bool   // Fetch all tags
	Branch    string // Specific branch to fetch (used only in FetchWithOptions)
	DryRun    bool   // Don't actually fetch, just show what would be done
	Progress  bool   // Show progress output
	Prune     bool   // Remove remote refs that don't exist locally
}

// logRequest logs the details of an HTTP request
func logRequest(req *http.Request) {
	log.Printf("[HTTP Request] %s %s", req.Method, req.URL.String())
	log.Printf("[HTTP Request] Headers:")
	for name, values := range req.Header {
		// Don't log the full authorization token for security reasons
		if strings.ToLower(name) == "authorization" {
			log.Printf("[HTTP Request]   %s: Bearer [TOKEN REDACTED]", name)
		} else {
			log.Printf("[HTTP Request]   %s: %s", name, values)
		}
	}
	if req.Body != nil {
		log.Printf("[HTTP Request] Has Body: true")
	} else {
		log.Printf("[HTTP Request] Has Body: false")
	}
}

// logResponse logs the details of an HTTP response
func logResponse(resp *http.Response) {
	respDump, err := httputil.DumpResponse(resp, true)
	if err != nil {
		log.Printf("[HTTP Response] Error dumping response: %v", err)
		return
	}

	// Limit the size of the dumped response to avoid overwhelming logs
	maxSize := 2000
	respLog := string(respDump)
	if len(respLog) > maxSize {
		respLog = respLog[:maxSize] + "... [truncated]"
	}

	log.Printf("[HTTP Response] Status: %s\n%s", resp.Status, respLog)
}

// FetchWithOptions fetches from a remote with additional options
func FetchWithOptions(repoRoot, remoteName string, opts FetchOptions) error {
	if !opts.Quiet && opts.Verbose {
		log.Printf("[Fetch] Starting fetch from remote '%s' with options: %+v", remoteName, opts)
	}

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

	if !opts.Quiet && opts.Verbose {
		log.Printf("[Fetch] Using remote URL: %s", remoteURL)
	}

	// Fetch remote refs
	refs, err := fetchRemoteRefs(remoteURL, remoteName, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch remote refs: %w", err)
	}

	if !opts.Quiet && opts.Verbose {
		log.Printf("[Fetch] Retrieved %d refs from remote", len(refs))
	}

	// Handle specific branch filter if provided in options
	if opts.Branch != "" {
		// Use the specific branch fetch function
		return FetchBranchWithOptions(repoRoot, remoteName, opts.Branch, opts)
	}

	// Get local refs for negotiation
	localRefs, err := getLocalRefs(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to get local refs: %w", err)
	}

	if !opts.Quiet && opts.Verbose {
		log.Printf("[Fetch] Found %d local refs", len(localRefs))
	}

	// Check for prune case - identify remote refs that should be pruned
	var refsToRemove []string
	if opts.Prune {
		refsToRemove = identifyRefsToRemove(repoRoot, remoteName, refs)
		if !opts.Quiet && opts.Verbose && len(refsToRemove) > 0 {
			log.Printf("[Fetch] Found %d remote refs to prune", len(refsToRemove))
		}

		// Perform pruning if not in dry-run mode
		if !opts.DryRun && len(refsToRemove) > 0 {
			if err := pruneRemoteRefs(repoRoot, remoteName, refsToRemove); err != nil {
				log.Printf("[Fetch] Warning: pruning failed: %v", err)
				// Continue with fetch even if pruning fails
			} else if !opts.Quiet {
				fmt.Printf("Pruned %d stale remote-tracking branches\n", len(refsToRemove))
			}
		} else if opts.DryRun && len(refsToRemove) > 0 && !opts.Quiet {
			fmt.Printf("Would prune %d stale remote-tracking branches\n", len(refsToRemove))
		}
	}

	// Negotiate with the server to determine missing objects
	missingObjects, err := negotiateFetch(remoteURL, remoteName, refs, localRefs, cfg)
	if err != nil {
		return fmt.Errorf("failed to negotiate fetch: %w", err)
	}

	if !opts.Quiet && opts.Verbose {
		log.Printf("[Fetch] Negotiation complete, %d objects missing", len(missingObjects))
	}

	if len(missingObjects) == 0 {
		if !opts.Quiet {
			fmt.Println("Already up to date.")
		}
		return nil
	}

	// In dry-run mode, just report what would be done
	if opts.DryRun {
		if !opts.Quiet {
			fmt.Printf("Would fetch %d objects from remote '%s'\n", len(missingObjects), remoteName)
			fmt.Printf("Would update %d remote-tracking references\n", len(refs))
		}
		return nil
	}

	// Apply depth limit if specified
	if opts.Depth > 0 && !opts.Quiet && opts.Verbose {
		log.Printf("[Fetch] Limiting history to depth %d", opts.Depth)
		// Note: Actual depth limitation would be implemented here
		// This would involve modifying the packfile request to include depth information
	}

	// Fetch the packfile containing missing objects
	if !opts.Quiet && opts.Progress {
		fmt.Printf("Downloading objects: %d object(s)\n", len(missingObjects))
	}

	packfile, err := fetchPackfile(remoteURL, remoteName, missingObjects, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch packfile: %w", err)
	}

	if !opts.Quiet && opts.Verbose {
		log.Printf("[Fetch] Received packfile of size %d bytes", len(packfile))
	}

	// Unpack the packfile
	if !opts.Quiet && opts.Progress {
		fmt.Printf("Unpacking objects: 100%% (%d/%d)\n", len(missingObjects), len(missingObjects))
	}

	if err := unpackPackfile(repoRoot, packfile); err != nil {
		return fmt.Errorf("failed to unpack packfile: %w", err)
	}

	// Update local tracking refs
	updatedRefs := 0
	for branch, commitHash := range refs {
		// Skip non-branch refs if only want branches
		if !opts.FetchTags && !strings.HasPrefix(branch, "refs/heads/") {
			continue
		}

		refPath := filepath.Join(repoRoot, ".vec", "refs", "remotes", remoteName, branch)
		if err := os.MkdirAll(filepath.Dir(refPath), 0755); err != nil {
			return fmt.Errorf("failed to create refs directory: %w", err)
		}

		if err := os.WriteFile(refPath, []byte(commitHash+"\n"), 0644); err != nil {
			return fmt.Errorf("failed to update tracking ref for %s: %w", branch, err)
		}

		updatedRefs++
		if !opts.Quiet && opts.Verbose {
			log.Printf("[Fetch] Updated tracking ref for %s to %s", branch, commitHash)
		}
	}

	if !opts.Quiet {
		fmt.Printf("Updated %d remote-tracking references\n", updatedRefs)
	}

	return nil
}

// FetchBranchWithOptions fetches a specific branch from the remote with additional options
func FetchBranchWithOptions(repoRoot, remoteName, branch string, opts FetchOptions) error {
	if !opts.Quiet && opts.Verbose {
		log.Printf("[FetchBranch] Starting fetch of branch '%s' from remote '%s'", branch, remoteName)
	}

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

	if !opts.Quiet && opts.Verbose {
		log.Printf("[FetchBranch] Using remote URL: %s", remoteURL)
	}

	// Fetch remote refs
	refs, err := fetchRemoteRefs(remoteURL, remoteName, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch remote refs: %w", err)
	}

	// Ensure the specific branch exists on the remote
	branchFullRef := branch
	// If the branch doesn't start with refs/, assume it's a regular branch name
	if !strings.HasPrefix(branch, "refs/") {
		branchFullRef = "refs/heads/" + branch
	}

	remoteHash, exists := refs[branchFullRef]
	if !exists {
		return fmt.Errorf("branch '%s' not found on remote", branch)
	}

	if !opts.Quiet && opts.Verbose {
		log.Printf("[FetchBranch] Found branch '%s' at commit %s", branch, remoteHash)
	}

	// Get local refs for negotiation
	localRefs, err := getLocalRefs(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to get local refs: %w", err)
	}

	// Restrict negotiation to the selected branch only
	remoteBranchRef := map[string]string{branchFullRef: remoteHash}
	localBranchRef := map[string]string{}

	// Check if we already have this branch locally
	if localHash, ok := localRefs[branchFullRef]; ok {
		localBranchRef[branchFullRef] = localHash
		if !opts.Quiet && opts.Verbose {
			log.Printf("[FetchBranch] Local branch exists at commit %s", localHash)
		}
	}

	// In dry-run mode, just report
	if opts.DryRun {
		if !opts.Quiet {
			fmt.Printf("Would fetch branch '%s' from remote '%s'\n", branch, remoteName)
		}
		return nil
	}

	// Negotiate with the server to determine missing objects for the specific branch
	missingObjects, err := negotiateFetch(remoteURL, remoteName, remoteBranchRef, localBranchRef, cfg)
	if err != nil {
		return fmt.Errorf("failed to negotiate fetch for branch '%s': %w", branch, err)
	}

	if len(missingObjects) == 0 {
		if !opts.Quiet {
			fmt.Printf("Branch '%s' is already up to date.\n", branch)
		}
		return nil
	}

	if !opts.Quiet && opts.Verbose {
		log.Printf("[FetchBranch] Need to fetch %d objects for branch '%s'", len(missingObjects), branch)
	}

	// Fetch the packfile containing missing objects
	if !opts.Quiet && opts.Progress {
		fmt.Printf("Downloading objects for branch '%s': %d object(s)\n", branch, len(missingObjects))
	}

	packfile, err := fetchPackfile(remoteURL, remoteName, missingObjects, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch packfile for branch '%s': %w", branch, err)
	}

	// Unpack the packfile
	if !opts.Quiet && opts.Progress {
		fmt.Printf("Unpacking objects: 100%% (%d/%d)\n", len(missingObjects), len(missingObjects))
	}

	if err := unpackPackfile(repoRoot, packfile); err != nil {
		return fmt.Errorf("failed to unpack packfile for branch '%s': %w", branch, err)
	}

	// Update local tracking ref for the specific branch
	// Ensure directory exists
	refDir := filepath.Join(repoRoot, ".vec", "refs", "remotes", remoteName)
	if err := os.MkdirAll(refDir, 0755); err != nil {
		return fmt.Errorf("failed to create refs directory: %w", err)
	}

	// Get the simple branch name without refs/heads/ prefix
	shortBranchName := branch
	if strings.HasPrefix(branch, "refs/heads/") {
		shortBranchName = strings.TrimPrefix(branch, "refs/heads/")
	}

	// Write the remote tracking reference
	refPath := filepath.Join(refDir, shortBranchName)
	if err := os.WriteFile(refPath, []byte(remoteHash+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to update tracking ref for branch '%s': %w", shortBranchName, err)
	}

	if !opts.Quiet {
		fmt.Printf("Branch '%s' updated to %s\n", shortBranchName, remoteHash[:8])
	}

	return nil
}

// identifyRefsToRemove identifies remote tracking refs that no longer exist on the remote
func identifyRefsToRemove(repoRoot, remoteName string, currentRemoteRefs map[string]string) []string {
	var refsToRemove []string

	// Get the directory containing remote tracking refs
	remoteRefsDir := filepath.Join(repoRoot, ".vec", "refs", "remotes", remoteName)
	if !utils.FileExists(remoteRefsDir) {
		return refsToRemove // No remote refs yet
	}

	// Walk the remote tracking refs
	filepath.Walk(remoteRefsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip on error
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Get relative path to determine ref name
		relPath, err := filepath.Rel(remoteRefsDir, path)
		if err != nil {
			return nil
		}

		// Convert to the remote ref format
		refName := "refs/heads/" + relPath

		// If this ref no longer exists in the current remote refs, mark for removal
		if _, exists := currentRemoteRefs[refName]; !exists {
			refsToRemove = append(refsToRemove, relPath)
		}

		return nil
	})

	return refsToRemove
}

// pruneRemoteRefs removes remote tracking refs that no longer exist on the remote
func pruneRemoteRefs(repoRoot, remoteName string, refsToRemove []string) error {
	for _, ref := range refsToRemove {
		refPath := filepath.Join(repoRoot, ".vec", "refs", "remotes", remoteName, ref)
		if err := os.Remove(refPath); err != nil {
			return fmt.Errorf("failed to remove stale ref '%s': %w", ref, err)
		}
		log.Printf("[Prune] Removed stale remote-tracking branch '%s'", ref)
	}
	return nil
}

// Fetch is the original function (now it uses FetchWithOptions)
func Fetch(repoRoot, remoteName string) error {
	return FetchWithOptions(repoRoot, remoteName, FetchOptions{
		Progress: true,
	})
}

// FetchBranch is the original function (now it uses FetchBranchWithOptions)
func FetchBranch(repoRoot, remoteName, branch string) error {
	return FetchBranchWithOptions(repoRoot, remoteName, branch, FetchOptions{
		Progress: true,
	})
}

// fetchRemoteRefs retrieves the branch refs from the remote with retry logic
func fetchRemoteRefs(remoteURL, remoteName string, cfg *config.Config) (map[string]string, error) {
	log.Printf("[fetchRemoteRefs] Fetching refs from endpoint: %s", vechttp.EndpointRefs)

	return vechttp.FetchRemoteRefs(remoteURL, remoteName, cfg)
}

// getLocalRefs retrieves local refs for negotiation
func getLocalRefs(repoRoot string) (map[string]string, error) {
	refs := make(map[string]string)
	refDir := filepath.Join(repoRoot, ".vec", "refs", "heads")
	if !utils.FileExists(refDir) {
		return refs, nil // No local refs yet
	}

	err := filepath.Walk(refDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relPath, _ := filepath.Rel(refDir, path)
			refs[relPath] = string(bytes.TrimSpace(data))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk local refs: %w", err)
	}
	return refs, nil
}

// negotiateFetch determines which objects are missing by negotiating with the server
func negotiateFetch(remoteURL, remoteName string, remoteRefs, localRefs map[string]string, cfg *config.Config) ([]string, error) {
	log.Printf("[negotiateFetch] Starting negotiation for %d remote refs against %d local refs",
		len(remoteRefs), len(localRefs))

	return vechttp.NegotiateFetch(remoteURL, remoteName, remoteRefs, localRefs, cfg)
}

// fetchPackfile retrieves a packfile containing the specified objects
func fetchPackfile(remoteURL, remoteName string, objectsList []string, cfg *config.Config) ([]byte, error) {
	log.Printf("[fetchPackfile] Fetching packfile for %d objects", len(objectsList))

	return vechttp.FetchPackfile(remoteURL, remoteName, objectsList, cfg)
}

// unpackPackfile unpacks the packfile into the local object store with integrity verification
func unpackPackfile(repoRoot string, packfile []byte) error {
	fmt.Printf("Unpacking packfile (%d bytes)...\n", len(packfile))

	// Verify packfile format first
	if len(packfile) < 12 { // Header (8) + at least some content + checksum (32)
		return fmt.Errorf("invalid packfile: too short (%d bytes)", len(packfile))
	}

	// Check for PACK signature
	if string(packfile[:4]) != "PACK" {
		// Try alternative format before failing
		if objects, err := objects.ParsePackfile(packfile); err == nil {
			fmt.Println("Using legacy packfile format")
			return saveObjects(repoRoot, objects)
		}
		return fmt.Errorf("invalid packfile: missing PACK signature")
	}

	// Create a temporary file for the packfile
	packfileTempFile, err := os.CreateTemp("", "vec-packfile-*.pack")
	if err != nil {
		return fmt.Errorf("failed to create temporary packfile: %w", err)
	}
	packfilePath := packfileTempFile.Name()

	// Make sure we clean up the temporary files when we're done
	defer func() {
		packfileTempFile.Close()
		os.Remove(packfilePath)
		// Also remove index file if it was created
		indexPath := packfilePath + ".idx"
		if utils.FileExists(indexPath) {
			os.Remove(indexPath)
		}
	}()

	// Write the packfile data to the temporary file
	if _, err := packfileTempFile.Write(packfile); err != nil {
		return fmt.Errorf("failed to write packfile to temporary file: %w", err)
	}

	// Flush data to disk
	if err := packfileTempFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync packfile data: %w", err)
	}

	// Verify packfile integrity by checking its checksum before processing
	if len(packfile) > 32 { // Minimum size for a packfile with checksum
		// Extract the checksum from the end of the packfile
		storedChecksum := packfile[len(packfile)-32:]

		// Compute checksum of the packfile without the checksum itself
		hasher := sha256.New()
		hasher.Write(packfile[:len(packfile)-32])
		computedChecksum := hasher.Sum(nil)

		// Compare checksums
		if !bytes.Equal(storedChecksum, computedChecksum) {
			return fmt.Errorf("packfile checksum verification failed")
		}

		fmt.Println("Packfile integrity verified")
	}

	// Close the file so ParseModernPackfile can open it
	packfileTempFile.Close()

	// Use improved ParseModernPackfile to parse the packfile with delta support
	parsedObjects, err := objects.ParseModernPackfile(packfilePath, true)
	if err != nil {
		// If modern parsing fails, try falling back to the original parser for backward compatibility
		fmt.Println("Modern packfile parsing failed, trying legacy format...")
		parsedObjects, err = objects.ParsePackfile(packfile)
		if err != nil {
			return fmt.Errorf("failed to parse packfile: %w", err)
		}
	}

	// Save the extracted objects
	return saveObjects(repoRoot, parsedObjects)
}

// saveObjects saves a collection of objects to the repository
func saveObjects(repoRoot string, objectsList []objects.Object) error {
	// Save each parsed object to the object store
	objectsImported := 0
	skippedObjects := 0

	// Create progress indicators for large imports
	showProgress := len(objectsList) > 100
	var progressInterval int

	if showProgress {
		fmt.Printf("Importing %d objects...\n", len(objectsList))
		progressInterval = len(objectsList) / 10 // Show progress at 10% intervals
		if progressInterval < 1 {
			progressInterval = 1
		}
	}

	for i, obj := range objectsList {
		// Build object path in .vec/objects directory using the hash
		objPath := filepath.Join(repoRoot, ".vec", "objects", obj.Hash[:2], obj.Hash[2:])

		// Skip if the object already exists
		if utils.FileExists(objPath) {
			skippedObjects++
			continue
		}

		// Show progress periodically
		if showProgress && i%progressInterval == 0 {
			fmt.Printf("Progress: %d%% (%d/%d objects)\n", i*100/len(objectsList), i, len(objectsList))
		}

		// Create directories if they don't exist
		if err := os.MkdirAll(filepath.Dir(objPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for object %s: %w", obj.Hash, err)
		}

		// Prepare object header based on type (using byte value)
		var objType string
		switch obj.Type {
		case 1: // Commit
			objType = "commit"
		case 2: // Tree
			objType = "tree"
		case 3: // Blob
			objType = "blob"
		default:
			return fmt.Errorf("unknown object type: %d", obj.Type)
		}

		header := fmt.Sprintf("%s %d\x00", objType, len(obj.Data))

		// Write object with header
		f, err := os.Create(objPath)
		if err != nil {
			return fmt.Errorf("failed to create object file %s: %w", obj.Hash, err)
		}

		// Create zlib writer
		zw := zlib.NewWriter(f)

		// Write header + data
		if _, err := zw.Write([]byte(header)); err != nil {
			zw.Close()
			f.Close()
			return fmt.Errorf("failed to write object header for %s: %w", obj.Hash, err)
		}

		if _, err := zw.Write(obj.Data); err != nil {
			zw.Close()
			f.Close()
			return fmt.Errorf("failed to write object data for %s: %w", obj.Hash, err)
		}

		// Close writers
		if err := zw.Close(); err != nil {
			f.Close()
			return fmt.Errorf("failed to close zlib writer for %s: %w", obj.Hash, err)
		}

		if err := f.Close(); err != nil {
			return fmt.Errorf("failed to close object file %s: %w", obj.Hash, err)
		}

		objectsImported++
	}

	fmt.Printf("Successfully imported %d objects (%d already existed)\n", objectsImported, skippedObjects)
	return nil
}

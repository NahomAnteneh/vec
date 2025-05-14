// internal/remote/fetch.go
package remote

import (
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"

	"compress/zlib"

	"sync"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/config"
	"github.com/NahomAnteneh/vec/internal/packfile"
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

// FetchWithOptionsRepo fetches from a remote with additional options using Repository context
func FetchWithOptionsRepo(repo *core.Repository, remoteName string, opts FetchOptions) error {
	if !opts.Quiet && opts.Verbose {
		log.Printf("[Fetch] Starting fetch from remote '%s' with options: %+v", remoteName, opts)
	}

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
		return FetchBranchWithOptionsRepo(repo, remoteName, opts.Branch, opts)
	}

	// Get local refs for negotiation
	localRefs, err := getLocalRefsRepo(repo)
	if err != nil {
		return fmt.Errorf("failed to get local refs: %w", err)
	}

	if !opts.Quiet && opts.Verbose {
		log.Printf("[Fetch] Found %d local refs", len(localRefs))
	}

	// Check for prune case - identify remote refs that should be pruned
	var refsToRemove []string
	if opts.Prune {
		refsToRemove = identifyRefsToRemoveRepo(repo, remoteName, refs)
		if !opts.Quiet && opts.Verbose && len(refsToRemove) > 0 {
			log.Printf("[Fetch] Found %d remote refs to prune", len(refsToRemove))
		}

		// Perform pruning if not in dry-run mode
		if !opts.DryRun && len(refsToRemove) > 0 {
			if err := pruneRemoteRefsRepo(repo, remoteName, refsToRemove); err != nil {
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

	if err := unpackPackfileRepo(repo, packfile); err != nil {
		return fmt.Errorf("failed to unpack packfile: %w", err)
	}

	// Update local tracking refs
	updatedRefs := 0
	for refName, hash := range refs {
		// Skip HEAD ref
		if refName == "HEAD" {
			continue
		}

		// Convert remote ref name to local tracking ref
		localRef := fmt.Sprintf("refs/remotes/%s/%s", remoteName, strings.TrimPrefix(refName, "refs/heads/"))

		// Get current value of local ref, if it exists
		oldHash := ""
		localRefPath := filepath.Join(repo.VecDir, localRef)
		if utils.FileExists(localRefPath) {
			content, err := os.ReadFile(localRefPath)
			if err == nil {
				oldHash = strings.TrimSpace(string(content))
			}
		}

		// Skip update if hash hasn't changed and not forced
		if oldHash == hash && !opts.Force {
			continue
		}

		// Ensure directory exists
		refDir := filepath.Dir(localRefPath)
		if err := os.MkdirAll(refDir, 0755); err != nil {
			return fmt.Errorf("failed to create ref directory: %w", err)
		}

		// Write new ref
		if err := os.WriteFile(localRefPath, []byte(hash+"\n"), 0644); err != nil {
			return fmt.Errorf("failed to update local ref %s: %w", localRef, err)
		}

		if !opts.Quiet && opts.Verbose {
			if oldHash == "" {
				fmt.Printf("* [new branch]      %s -> %s\n", strings.TrimPrefix(refName, "refs/heads/"), strings.TrimPrefix(localRef, "refs/remotes/"))
			} else {
				fmt.Printf("* [updated]         %s -> %s\n", strings.TrimPrefix(refName, "refs/heads/"), strings.TrimPrefix(localRef, "refs/remotes/"))
			}
		}

		updatedRefs++
	}

	if !opts.Quiet && !opts.Verbose {
		fmt.Printf("Updated %d reference(s)\n", updatedRefs)
	}

	return nil
}

// FetchBranchWithOptionsRepo fetches a specific branch from a remote using Repository context
func FetchBranchWithOptionsRepo(repo *core.Repository, remoteName, branch string, opts FetchOptions) error {
	if !opts.Quiet && opts.Verbose {
		log.Printf("[Fetch] Starting fetch of branch '%s' from remote '%s'", branch, remoteName)
	}

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

	// Prepare the branch reference name
	branchRef := "refs/heads/" + branch

	// Fetch remote refs
	refs, err := fetchRemoteRefs(remoteURL, remoteName, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch remote refs: %w", err)
	}

	// Check if the branch exists on the remote
	if _, exists := refs[branchRef]; !exists {
		return fmt.Errorf("branch '%s' not found on remote '%s'", branch, remoteName)
	}

	// Get local refs for negotiation
	localRefs, err := getLocalRefsRepo(repo)
	if err != nil {
		return fmt.Errorf("failed to get local refs: %w", err)
	}

	// Filter remote refs to only include the requested branch
	filteredRefs := make(map[string]string)
	filteredRefs[branchRef] = refs[branchRef]

	// Negotiate with the server to determine missing objects
	missingObjects, err := negotiateFetch(remoteURL, remoteName, filteredRefs, localRefs, cfg)
	if err != nil {
		return fmt.Errorf("failed to negotiate fetch: %w", err)
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
			fmt.Printf("Would fetch branch '%s' from remote '%s' (%d objects)\n", branch, remoteName, len(missingObjects))
		}
		return nil
	}

	// Fetch the packfile containing missing objects
	if !opts.Quiet && opts.Progress {
		fmt.Printf("Downloading objects: %d object(s) for branch '%s'\n", len(missingObjects), branch)
	}

	packfileData, err := fetchPackfile(remoteURL, remoteName, missingObjects, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch packfile: %w", err)
	}

	// Unpack the packfile
	if !opts.Quiet && opts.Progress {
		fmt.Printf("Unpacking objects: 100%% (%d/%d)\n", len(missingObjects), len(missingObjects))
	}

	if err := unpackPackfileRepo(repo, packfileData); err != nil {
		return fmt.Errorf("failed to unpack packfile: %w", err)
	}

	// Update the local tracking ref for this branch
	localRef := fmt.Sprintf("refs/remotes/%s/%s", remoteName, branch)
	localRefPath := filepath.Join(repo.VecDir, localRef)

	// Ensure directory exists
	refDir := filepath.Dir(localRefPath)
	if err := os.MkdirAll(refDir, 0755); err != nil {
		return fmt.Errorf("failed to create ref directory: %w", err)
	}

	// Write new ref
	if err := os.WriteFile(localRefPath, []byte(refs[branchRef]+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to update local ref %s: %w", localRef, err)
	}

	if !opts.Quiet {
		fmt.Printf("Updated branch '%s' from remote '%s'\n", branch, remoteName)
	}

	return nil
}

// Legacy functions for backward compatibility

func FetchWithOptions(repoRoot, remoteName string, opts FetchOptions) error {
	repo := core.NewRepository(repoRoot)
	return FetchWithOptionsRepo(repo, remoteName, opts)
}

func FetchBranchWithOptions(repoRoot, remoteName, branch string, opts FetchOptions) error {
	repo := core.NewRepository(repoRoot)
	return FetchBranchWithOptionsRepo(repo, remoteName, branch, opts)
}

// Helper functions using Repository context

func getLocalRefsRepo(repo *core.Repository) (map[string]string, error) {
	refs := make(map[string]string)

	// Get all branch refs
	branchesDir := filepath.Join(repo.VecDir, "refs", "heads")
	if utils.FileExists(branchesDir) {
		err := filepath.Walk(branchesDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(branchesDir, path)
			if err != nil {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			refs["refs/heads/"+rel] = strings.TrimSpace(string(content))
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to read branch refs: %w", err)
		}
	}

	// Get all remote refs
	remotesDir := filepath.Join(repo.VecDir, "refs", "remotes")
	if utils.FileExists(remotesDir) {
		err := filepath.Walk(remotesDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(remotesDir, path)
			if err != nil {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			refs["refs/remotes/"+rel] = strings.TrimSpace(string(content))
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to read remote refs: %w", err)
		}
	}

	return refs, nil
}

func identifyRefsToRemoveRepo(repo *core.Repository, remoteName string, currentRemoteRefs map[string]string) []string {
	var refsToRemove []string

	// Get the local directory for this remote's refs
	remoteRefsDir := filepath.Join(repo.VecDir, "refs", "remotes", remoteName)
	if !utils.FileExists(remoteRefsDir) {
		return refsToRemove
	}

	// Walk the local refs directory for this remote
	filepath.Walk(remoteRefsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if info.IsDir() {
			return nil // Skip directories
		}

		// Get the relative path
		relPath, err := filepath.Rel(remoteRefsDir, path)
		if err != nil {
			return nil
		}

		// Check if this local remote-tracking ref still exists in the remote
		remoteRefName := "refs/heads/" + relPath
		if _, exists := currentRemoteRefs[remoteRefName]; !exists {
			refsToRemove = append(refsToRemove, relPath)
		}

		return nil
	})

	return refsToRemove
}

func pruneRemoteRefsRepo(repo *core.Repository, remoteName string, refsToRemove []string) error {
	for _, ref := range refsToRemove {
		refPath := filepath.Join(repo.VecDir, "refs", "remotes", remoteName, ref)
		if utils.FileExists(refPath) {
			if err := os.Remove(refPath); err != nil {
				return fmt.Errorf("failed to remove ref %s: %w", ref, err)
			}
		}
	}

	return nil
}

func unpackPackfileRepo(repo *core.Repository, packfileData []byte) error {
	// Create a temporary file for the packfile
	tmpFile, err := os.CreateTemp("", "vec-packfile-*.pack")
	if err != nil {
		return fmt.Errorf("failed to create temporary packfile: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write packfile data to temporary file
	if _, err := tmpFile.Write(packfileData); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write packfile data: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary packfile: %w", err)
	}

	// Extract objects from packfile
	objects, err := packfile.ParseModernPackfile(tmpFile.Name(), true)
	if err != nil {
		// If modern parsing fails, try falling back to the original parser
		objects, err = packfile.ParsePackfile(packfileData)
		if err != nil {
			return fmt.Errorf("failed to parse packfile: %w", err)
		}
	}

	// Save extracted objects
	if err := saveObjectsRepo(repo, objects); err != nil {
		return fmt.Errorf("failed to save objects: %w", err)
	}

	return nil
}

func saveObjectsRepo(repo *core.Repository, objectsList []packfile.Object) error {
	// Create a channel to limit concurrency
	semaphore := make(chan struct{}, 10)
	var wg sync.WaitGroup

	// Create a channel for errors
	errorCh := make(chan error, len(objectsList))

	// Process each object
	for _, obj := range objectsList {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(object packfile.Object) {
			defer wg.Done()
			defer func() { <-semaphore }()

			// Calculate hash (using object's data and header)
			hash := sha256.Sum256(append([]byte(fmt.Sprintf("%s %d\x00", object.Type, len(object.Data))), object.Data...))
			hashStr := fmt.Sprintf("%x", hash)

			// Prepare object path
			objDir := filepath.Join(repo.VecDir, "objects", hashStr[:2])
			objPath := filepath.Join(objDir, hashStr[2:])

			// Skip if object already exists
			if utils.FileExists(objPath) {
				return
			}

			// Create directory if it doesn't exist
			if err := os.MkdirAll(objDir, 0755); err != nil {
				errorCh <- fmt.Errorf("failed to create object directory: %w", err)
				return
			}

			// Create object file
			file, err := os.Create(objPath)
			if err != nil {
				errorCh <- fmt.Errorf("failed to create object file: %w", err)
				return
			}
			defer file.Close()

			// Compress object data
			zw := zlib.NewWriter(file)
			header := []byte(fmt.Sprintf("%s %d\x00", object.Type, len(object.Data)))
			if _, err := zw.Write(append(header, object.Data...)); err != nil {
				errorCh <- fmt.Errorf("failed to compress object data: %w", err)
				return
			}

			if err := zw.Close(); err != nil {
				errorCh <- fmt.Errorf("failed to finalize compressed data: %w", err)
				return
			}
		}(obj)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Check for errors
	close(errorCh)
	for err := range errorCh {
		if err != nil {
			return err
		}
	}

	return nil
}

// fetchRemoteRefs retrieves the branch refs from the remote with retry logic
func fetchRemoteRefs(remoteURL, remoteName string, cfg *config.Config) (map[string]string, error) {
	log.Printf("[fetchRemoteRefs] Fetching refs from endpoint: %s", vechttp.EndpointRefs)

	return vechttp.FetchRemoteRefs(remoteURL, remoteName, cfg)
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

// internal/remote/fetch.go
package remote

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/config"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/utils"
)

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

// Fetch retrieves refs and objects from the remote repository efficiently
func Fetch(repoRoot, remoteName string) error {
	log.Printf("[Fetch] Starting fetch from remote '%s'", remoteName)

	// Load config
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Debug config
	log.Printf("[Fetch] Config loaded from %s", repoRoot)
	if remote, exists := cfg.Remotes[remoteName]; exists {
		log.Printf("[Fetch] Remote '%s' found in config: URL=%s, Auth=%v",
			remoteName, remote.URL, remote.Auth != "")
	} else {
		log.Printf("[Fetch] Remote '%s' not found in loaded config", remoteName)
	}

	// Get remote URL
	remoteURL, err := cfg.GetRemoteURL(remoteName)
	if err != nil {
		return fmt.Errorf("failed to get remote URL: %w", err)
	}

	log.Printf("[Fetch] Using remote URL: %s", remoteURL)

	// Fetch remote refs
	refs, err := fetchRemoteRefs(remoteURL, remoteName, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch remote refs: %w", err)
	}

	log.Printf("[Fetch] Retrieved %d refs from remote", len(refs))

	// Get local refs for negotiation
	localRefs, err := getLocalRefs(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to get local refs: %w", err)
	}

	log.Printf("[Fetch] Found %d local refs", len(localRefs))

	// Negotiate with the server to determine missing objects
	missingObjects, err := negotiateFetch(remoteURL, remoteName, refs, localRefs, cfg)
	if err != nil {
		return fmt.Errorf("failed to negotiate fetch: %w", err)
	}

	log.Printf("[Fetch] Negotiation complete, %d objects missing", len(missingObjects))

	if len(missingObjects) == 0 {
		fmt.Println("Already up to date.")
		return nil
	}

	// Fetch the packfile containing missing objects
	packfile, err := fetchPackfile(remoteURL, remoteName, missingObjects, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch packfile: %w", err)
	}

	log.Printf("[Fetch] Received packfile of size %d bytes", len(packfile))

	// Unpack the packfile using production logic
	if err := unpackPackfile(repoRoot, packfile); err != nil {
		return fmt.Errorf("failed to unpack packfile: %w", err)
	}

	// Update local tracking refs
	for branch, commitHash := range refs {
		refPath := filepath.Join(repoRoot, ".vec", "refs", "remotes", remoteName, branch)
		if err := os.MkdirAll(filepath.Dir(refPath), 0755); err != nil {
			return fmt.Errorf("failed to create refs directory: %w", err)
		}
		if err := os.WriteFile(refPath, []byte(commitHash+"\n"), 0644); err != nil {
			return fmt.Errorf("failed to update tracking ref for %s: %w", branch, err)
		}
		log.Printf("[Fetch] Updated tracking ref for %s to %s", branch, commitHash)
	}

	log.Printf("[Fetch] Fetch completed successfully for remote '%s'", remoteName)
	fmt.Println("Fetch completed successfully.")
	return nil
}

// FetchBranch fetches a specific branch from the remote repository.
func FetchBranch(repoRoot, remoteName, branch string) error {
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

	// Fetch remote refs
	refs, err := fetchRemoteRefs(remoteURL, remoteName, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch remote refs: %w", err)
	}

	// Ensure the specific branch exists on the remote
	remoteHash, exists := refs[branch]
	if !exists {
		return fmt.Errorf("branch '%s' not found on remote", branch)
	}

	// Get local refs for negotiation
	localRefs, err := getLocalRefs(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to get local refs: %w", err)
	}

	// Restrict negotiation to the selected branch only
	remoteBranchRef := map[string]string{branch: remoteHash}
	localBranchRef := map[string]string{}
	if localHash, ok := localRefs[branch]; ok {
		localBranchRef[branch] = localHash
	}

	// Negotiate with the server to determine missing objects for the specific branch
	missingObjects, err := negotiateFetch(remoteURL, remoteName, remoteBranchRef, localBranchRef, cfg)
	if err != nil {
		return fmt.Errorf("failed to negotiate fetch for branch '%s': %w", branch, err)
	}

	if len(missingObjects) == 0 {
		fmt.Printf("Branch '%s' is already up to date.\n", branch)
		return nil
	}

	// Fetch the packfile containing missing objects
	packfile, err := fetchPackfile(remoteURL, remoteName, missingObjects, cfg)
	if err != nil {
		return fmt.Errorf("failed to fetch packfile for branch '%s': %w", branch, err)
	}

	// Unpack the packfile using production logic
	if err := unpackPackfile(repoRoot, packfile); err != nil {
		return fmt.Errorf("failed to unpack packfile for branch '%s': %w", branch, err)
	}

	// Update local tracking ref for the specific branch
	refPath := filepath.Join(repoRoot, ".vec", "refs", "remotes", remoteName, branch)
	if err := os.MkdirAll(filepath.Dir(refPath), 0755); err != nil {
		return fmt.Errorf("failed to create refs directory: %w", err)
	}
	if err := os.WriteFile(refPath, []byte(remoteHash+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to update tracking ref for branch '%s': %w", branch, err)
	}

	fmt.Printf("Fetch for branch '%s' completed successfully.\n", branch)
	return nil
}

// fetchRemoteRefs retrieves the branch refs from the remote with retry logic
func fetchRemoteRefs(remoteURL, remoteName string, cfg *config.Config) (map[string]string, error) {
	// Update URL path to match server's structure - using /:username/:repo_slug/refs
	url := fmt.Sprintf("%s/refs", remoteURL)
	log.Printf("[fetchRemoteRefs] Fetching refs from %s", url)
	client := &http.Client{Timeout: 10 * time.Second}

	for attempt := 1; attempt <= 3; attempt++ {
		log.Printf("[fetchRemoteRefs] Attempt %d of 3", attempt)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Add authentication headers if available
		ApplyAuthHeaders(req, remoteName, cfg)

		// Log the request
		logRequest(req)

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[fetchRemoteRefs] Request error: %v", err)
			if attempt == 3 {
				return nil, fmt.Errorf("cannot contact remote after %d attempts: %w", attempt, err)
			}
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}

		// Log the response
		logResponse(resp)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("[fetchRemoteRefs] Error response: %d", resp.StatusCode)
			return nil, fmt.Errorf("refs fetch failed with status %d (URL: %s)", resp.StatusCode, url)
		}

		// Try to decode as a direct map first
		var refs map[string]string
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("[fetchRemoteRefs] Failed to read response body: %v", err)
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		// Try to decode as a direct map
		if err := json.Unmarshal(body, &refs); err != nil {
			// If that fails, try to decode as a wrapped object with a "refs" field
			log.Printf("[fetchRemoteRefs] Failed to decode as direct map, trying wrapped format: %v", err)
			var wrappedRefs struct {
				Refs map[string]string `json:"refs"`
			}
			if err := json.Unmarshal(body, &wrappedRefs); err != nil {
				log.Printf("[fetchRemoteRefs] Failed to decode response in any format: %v", err)
				return nil, fmt.Errorf("failed to decode refs: %w", err)
			}
			refs = wrappedRefs.Refs
		}

		log.Printf("[fetchRemoteRefs] Successfully fetched %d refs", len(refs))
		return refs, nil
	}
	return nil, fmt.Errorf("unexpected error in fetchRemoteRefs")
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

	negotiationData := map[string]interface{}{
		"want": remoteRefs,
		"have": localRefs,
	}

	// Use the correct URL format for the negotiate endpoint
	url := fmt.Sprintf("%s/negotiate", remoteURL)
	log.Printf("[negotiateFetch] Negotiating with endpoint: %s", url)

	data, err := json.Marshal(negotiationData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal negotiation data: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Add authentication headers if available
	ApplyAuthHeaders(req, remoteName, cfg)

	// Log the request
	logRequest(req)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[negotiateFetch] Request error: %v", err)
		return nil, fmt.Errorf("negotiation request failed: %w", err)
	}

	// Log the response
	logResponse(resp)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[negotiateFetch] Error response: %d", resp.StatusCode)
		return nil, fmt.Errorf("negotiation failed with status %d (URL: %s)", resp.StatusCode, url)
	}

	var missingObjects []string
	if err := json.NewDecoder(resp.Body).Decode(&missingObjects); err != nil {
		log.Printf("[negotiateFetch] Failed to decode response: %v", err)
		return nil, fmt.Errorf("failed to decode missing objects: %w", err)
	}
	log.Printf("[negotiateFetch] Negotiation complete, %d objects missing", len(missingObjects))
	return missingObjects, nil
}

// fetchPackfile retrieves a packfile containing the specified objects
func fetchPackfile(remoteURL, remoteName string, objectsList []string, cfg *config.Config) ([]byte, error) {
	// Use the correct URL format for the packfile endpoint
	url := fmt.Sprintf("%s/packfile", remoteURL)
	log.Printf("[fetchPackfile] Fetching packfile from %s for %d objects", url, len(objectsList))
	client := &http.Client{Timeout: 30 * time.Second}

	data, err := json.Marshal(objectsList)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal objects list: %w", err)
	}

	for attempt := 1; attempt <= 3; attempt++ {
		log.Printf("[fetchPackfile] Attempt %d of 3", attempt)
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		// Add authentication headers if available
		ApplyAuthHeaders(req, remoteName, cfg)

		// Log the request
		logRequest(req)

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[fetchPackfile] Request error: %v", err)
			if attempt == 3 {
				return nil, fmt.Errorf("failed to fetch packfile after %d attempts: %w", attempt, err)
			}
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}

		// Log the response headers (not the body as it might be large)
		log.Printf("[HTTP Response] Status: %s", resp.Status)
		log.Printf("[HTTP Response] Headers: %v", resp.Header)
		log.Printf("[HTTP Response] Content-Length: %s", resp.Header.Get("Content-Length"))
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("[fetchPackfile] Error response: %d", resp.StatusCode)
			return nil, fmt.Errorf("packfile fetch failed with status %d (URL: %s)", resp.StatusCode, url)
		}

		packfile, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("[fetchPackfile] Failed to read response body: %v", err)
			return nil, fmt.Errorf("failed to read packfile: %w", err)
		}
		log.Printf("[fetchPackfile] Successfully received packfile (%d bytes)", len(packfile))
		return packfile, nil
	}
	return nil, fmt.Errorf("unexpected error in fetchPackfile")
}

// getBaseURL extracts the base API URL from a repository-specific URL
func getBaseURL(repoURL string) string {
	// No need to strip any path components - we want to preserve the entire URL
	// to ensure the server knows which repository we're working with
	return repoURL
}

// unpackPackfile unpacks the packfile into the local object store using the modern packfile format
func unpackPackfile(repoRoot string, packfile []byte) error {
	fmt.Printf("Unpacking packfile (%d bytes)...\n", len(packfile))

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

	// Create an index file if needed
	// We'll check the packfile header to see if it's a modern format
	if len(packfile) >= 4 && string(packfile[:4]) == "PACK" {
		// Modern packfile format, create an index if not included
		// Rewind the file to the beginning for proper reading
		if _, err := packfileTempFile.Seek(0, 0); err != nil {
			return fmt.Errorf("failed to seek in packfile: %w", err)
		}

		// Read the header
		header := objects.PackFileHeader{}
		if err := binary.Read(packfileTempFile, binary.BigEndian, &header); err != nil {
			return fmt.Errorf("failed to read packfile header: %w", err)
		}

		// Create an index for the packfile
		indexPath := packfilePath + ".idx"

		// Only create the index if we have a modern packfile format and it's not already there
		if !utils.FileExists(indexPath) {
			fmt.Println("Creating index for packfile...")

			// Extract object information and create index
			index := objects.PackfileIndex{
				Entries: make(map[string]objects.PackIndexEntry),
			}

			// Use the parsed objects to create the index
			// For now, we will rely on ParseModernPackfile to handle this
			// In a full implementation, we should create the index properly here

			if err := objects.WritePackIndex(&index, indexPath); err != nil {
				return fmt.Errorf("failed to write packfile index: %w", err)
			}
		}
	}

	// Close the file so ParseModernPackfile can open it
	packfileTempFile.Close()

	// Use ParseModernPackfile to parse the packfile with delta support
	parsedObjects, err := objects.ParseModernPackfile(packfilePath, true)
	if err != nil {
		// If modern parsing fails, try falling back to the original parser for backward compatibility
		fmt.Println("Modern packfile parsing failed, trying legacy format...")
		parsedObjects, err = objects.ParsePackfile(packfile)
		if err != nil {
			return fmt.Errorf("failed to parse packfile: %w", err)
		}
	}

	// Save each parsed object to the object store
	objectsImported := 0
	for _, obj := range parsedObjects {
		objPath := objects.GetObjectPath(repoRoot, obj.Hash)

		// Skip if the object already exists
		if utils.FileExists(objPath) {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(objPath), 0755); err != nil {
			return fmt.Errorf("failed to create object directory: %w", err)
		}

		if err := os.WriteFile(objPath, obj.Data, 0644); err != nil {
			return fmt.Errorf("failed to write object %s: %w", obj.Hash, err)
		}

		objectsImported++
	}

	fmt.Printf("Unpacking completed. Imported %d new objects.\n", objectsImported)
	return nil
}

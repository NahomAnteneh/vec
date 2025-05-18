// Package http provides a HTTP client for Vec remote operations
package http

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
)

// Common constants
const (
	// Default timeout for HTTP requests
	DefaultTimeout = 60 * time.Second
	
	// Standard content type
	ContentTypeJSON = "application/json"
	ContentTypeGit = "application/x-git"
)

// Common error types
var (
	ErrNetworkError    = errors.New("network error occurred")
	ErrNotFound        = errors.New("resource not found")
)

// Auth handles authentication for HTTP requests
type Auth interface {
	ApplyAuth(req *http.Request) error
}

// BasicAuth implements basic username/password authentication
type BasicAuth struct {
	Username string
	Password string
}

// ApplyAuth applies basic authentication to the request
func (a *BasicAuth) ApplyAuth(req *http.Request) error {
	if a.Username != "" {
		req.SetBasicAuth(a.Username, a.Password)
	}
	return nil
}

// ConfigAuth loads authentication from configuration
type ConfigAuth struct {
	Config     *config.Config
	RemoteName string
}

// ApplyAuth applies authentication from config to the request
func (a *ConfigAuth) ApplyAuth(req *http.Request) error {
	if a.Config == nil {
		return nil
	}

	// Try username/password from config
	username, err := a.Config.GetValue(fmt.Sprintf("remote.%s.username", a.RemoteName))
	if err == nil && username != "" {
		password, err := a.Config.GetValue(fmt.Sprintf("remote.%s.password", a.RemoteName))
		if err == nil && password != "" {
			req.SetBasicAuth(username, password)
			return nil
		}
	}

	// Try credentials file as fallback
	creds, err := getCredentials(a.RemoteName)
	if err == nil && creds.Username != "" {
		req.SetBasicAuth(creds.Username, creds.Password)
	}

	return nil
}

// Credential holds user authentication information
type Credential struct {
	Username string
	Password string
}

// getCredentials retrieves credentials from the credentials file
func getCredentials(remoteName string) (*Credential, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	// Look for credentials in ~/.vec/credentials
	credsPath := filepath.Join(homeDir, ".vec", "credentials")
	if _, err := os.Stat(credsPath); os.IsNotExist(err) {
		return &Credential{}, nil
	}

	credsData, err := os.ReadFile(credsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	// Parse the credentials file
	lines := strings.Split(string(credsData), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Look for remote.{name}.username and remote.{name}.password
		if strings.HasPrefix(line, fmt.Sprintf("remote.%s.username=", remoteName)) {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				username := strings.TrimSpace(parts[1])
				
				// Look for corresponding password
				for _, passwordLine := range lines {
					passwordLine = strings.TrimSpace(passwordLine)
					if strings.HasPrefix(passwordLine, fmt.Sprintf("remote.%s.password=", remoteName)) {
						passParts := strings.SplitN(passwordLine, "=", 2)
						if len(passParts) == 2 {
							return &Credential{
								Username: username,
								Password: strings.TrimSpace(passParts[1]),
							}, nil
						}
					}
				}
				
				// Username found but no password
				return &Credential{
					Username: username,
				}, nil
			}
		}
	}

	return &Credential{}, nil
}

// Client represents a simple HTTP client for Vec remote operations
type Client struct {
	httpClient *http.Client
	remoteURL  string
	remoteName string
	config     *config.Config
	auth       Auth
	verbose    bool
}

// NewClient creates a new HTTP client
func NewClient(remoteURL, remoteName string, cfg *config.Config) *Client {
	client := &Client{
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		remoteURL:  remoteURL,
		remoteName: remoteName,
		config:     cfg,
		verbose:    false,
	}
	
	// Set default auth from config
	client.auth = &ConfigAuth{
		Config:     cfg,
		RemoteName: remoteName,
	}
	
	return client
}

// SetAuth sets a custom authentication mechanism
func (c *Client) SetAuth(auth Auth) {
	c.auth = auth
}

// SetTimeout sets the timeout for HTTP requests
func (c *Client) SetTimeout(timeout time.Duration) {
	c.httpClient.Timeout = timeout
}

// SetVerbose enables or disables verbose output
func (c *Client) SetVerbose(verbose bool) {
	c.verbose = verbose
}

// Get performs a GET request to the remote server
func (c *Client) Get(path string) ([]byte, error) {
	url := c.buildURL(path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Add authentication
	if c.auth != nil {
		if err := c.auth.ApplyAuth(req); err != nil {
			return nil, fmt.Errorf("failed to apply auth: %w", err)
		}
	}
	
	// Add standard headers
	req.Header.Set("User-Agent", "Vec-Client/1.0")
	req.Header.Set("Accept", ContentTypeJSON)
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetworkError, err)
	}
	defer resp.Body.Close()
	
	// Check for error responses
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned error: %d %s", resp.StatusCode, resp.Status)
	}
	
	return io.ReadAll(resp.Body)
}

// Post performs a POST request to the remote server
func (c *Client) Post(path string, data interface{}) ([]byte, error) {
	url := c.buildURL(path)
	
	var body io.Reader
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal data: %w", err)
		}
		body = bytes.NewBuffer(jsonData)
	}
	
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Add authentication
	if c.auth != nil {
		if err := c.auth.ApplyAuth(req); err != nil {
			return nil, fmt.Errorf("failed to apply auth: %w", err)
		}
	}
	
	// Add standard headers
	req.Header.Set("User-Agent", "Vec-Client/1.0")
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set("Accept", ContentTypeJSON)
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetworkError, err)
	}
	defer resp.Body.Close()
	
	// Check for error responses
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned error: %d %s", resp.StatusCode, resp.Status)
	}
	
	return io.ReadAll(resp.Body)
}

// PostBinary posts binary data (like packfiles) to the remote server
func (c *Client) PostBinary(path string, data []byte) ([]byte, error) {
	url := c.buildURL(path)
	
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Add authentication
	if c.auth != nil {
		if err := c.auth.ApplyAuth(req); err != nil {
			return nil, fmt.Errorf("failed to apply auth: %w", err)
		}
	}
	
	// Add standard headers
	req.Header.Set("User-Agent", "Vec-Client/1.0")
	req.Header.Set("Content-Type", ContentTypeGit)
	req.Header.Set("Accept", ContentTypeJSON)
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetworkError, err)
	}
	defer resp.Body.Close()
	
	// Check for error responses
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned error: %d %s", resp.StatusCode, resp.Status)
	}
	
	return io.ReadAll(resp.Body)
}

// buildURL creates the full URL for a request
func (c *Client) buildURL(path string) string {
	baseURL := strings.TrimRight(c.remoteURL, "/")
	path = strings.TrimLeft(path, "/")
	return fmt.Sprintf("%s/%s", baseURL, path)
}

// GetRefs retrieves all references from the remote repository
func (c *Client) GetRefs() (map[string]string, error) {
	data, err := c.Get("info/refs")
	if err != nil {
		return nil, err
	}
	
	refs := make(map[string]string)
	err = json.Unmarshal(data, &refs)
	if err != nil {
		return nil, fmt.Errorf("failed to parse refs data: %w", err)
	}
	
	return refs, nil
}

// GetObject retrieves an object from the remote repository
func (c *Client) GetObject(hash string) ([]byte, error) {
	return c.Get(fmt.Sprintf("objects/%s", hash))
}

// PushResult contains the result of a push operation
type PushResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Push sends a packfile to the remote repository
func (c *Client) Push(branchName, oldCommit, newCommit string, packfile []byte) (*PushResult, error) {
	// First send the push info
	pushInfo := map[string]string{
		"branch":    branchName,
		"oldCommit": oldCommit,
		"newCommit": newCommit,
	}
	
	infoData, err := c.Post("push/info", pushInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to send push info: %w", err)
	}
	
	// Parse response to check if we should continue
	var infoResult struct {
		Continue bool   `json:"continue"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(infoData, &infoResult); err != nil {
		return nil, fmt.Errorf("failed to parse push info response: %w", err)
	}
	
	if !infoResult.Continue {
		return &PushResult{
			Success: false,
			Message: infoResult.Message,
		}, nil
	}
	
	// Send the packfile
	resultData, err := c.PostBinary("push/packfile", packfile)
	if err != nil {
		return nil, fmt.Errorf("failed to send packfile: %w", err)
	}
	
	// Parse the result
	var result PushResult
	if err := json.Unmarshal(resultData, &result); err != nil {
		return nil, fmt.Errorf("failed to parse push result: %w", err)
	}
	
	return &result, nil
}

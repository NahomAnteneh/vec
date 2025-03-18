// Package http provides a centralized HTTP client for all Vec remote operations
package http

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/config"
)

// API version and constants
const (
	// API Version
	APIVersion = "v1"

	// Default timeout for HTTP requests
	DefaultTimeout = 60 * time.Second

	// Endpoint paths
	EndpointRefs         = "refs"
	EndpointBranches     = "branches"
	EndpointNegotiations = "negotiations"
	EndpointPackfiles    = "packfiles"
	EndpointPushes       = "pushes"

	// Content types
	ContentTypeJSON = "application/json"

	// Max retry attempts
	MaxRetryAttempts = 3
)

// Common error types
var (
	ErrNetworkError         = errors.New("network error occurred")
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrResourceNotFound     = errors.New("resource not found")
	ErrBadRequest           = errors.New("bad request")
	ErrServerError          = errors.New("server error")
)

// Client represents the HTTP client for Vec remote operations
type Client struct {
	httpClient *http.Client
	remoteURL  string
	remoteName string
	cfg        *config.Config
	verbose    bool
}

// ResponseResult wraps the HTTP response and any error that occurred
type ResponseResult struct {
	Response *http.Response
	Error    error
}

// NewClient creates a new HTTP client for Vec remote operations
func NewClient(remoteURL, remoteName string, cfg *config.Config) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		remoteURL:  remoteURL,
		remoteName: remoteName,
		cfg:        cfg,
		verbose:    false,
	}
}

// SetVerbose enables or disables verbose logging
func (c *Client) SetVerbose(verbose bool) {
	c.verbose = verbose
}

// SetTimeout sets the timeout for all requests
func (c *Client) SetTimeout(timeout time.Duration) {
	c.httpClient.Timeout = timeout
}

// buildURL creates a standardized URL for API requests
func (c *Client) buildURL(endpoint string, params ...string) string {
	baseURL := strings.TrimRight(c.remoteURL, "/")
	apiPath := fmt.Sprintf("api/%s/%s", APIVersion, strings.TrimLeft(endpoint, "/"))

	url := fmt.Sprintf("%s/%s", baseURL, apiPath)

	// Add any additional path parameters
	for _, param := range params {
		if param != "" {
			url = fmt.Sprintf("%s/%s", url, param)
		}
	}

	return url
}

// applyAuthHeaders adds authentication headers to the request
func (c *Client) applyAuthHeaders(req *http.Request) error {
	if c.cfg == nil {
		return nil
	}

	// Try to get auth token from config
	token, err := c.cfg.GetRemoteAuth(c.remoteName)
	if err == nil && token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		if c.verbose {
			log.Printf("Added authorization header for remote '%s'", c.remoteName)
		}
	}

	// Add any custom headers from config
	if c.cfg != nil {
		headers, err := c.cfg.GetRemoteHeaders(c.remoteName)
		if err == nil && headers != nil {
			for key, value := range headers {
				req.Header.Set(key, value)
			}
		}
	}

	return nil
}

// sendRequest sends an HTTP request with standardized headers, retries, and error handling
func (c *Client) sendRequest(method, endpoint string, data interface{}, params ...string) (*ResponseResult, error) {
	url := c.buildURL(endpoint, params...)

	// Prepare request body if data is provided
	var body io.Reader
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request data: %w", err)
		}
		body = bytes.NewBuffer(jsonData)
	}

	// Create request
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set standard headers for all requests
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set("Accept", ContentTypeJSON)
	req.Header.Set("User-Agent", "Vec-Client/0.1.0")
	req.Header.Set("X-Vec-API-Version", APIVersion)

	// Add authentication headers
	if err := c.applyAuthHeaders(req); err != nil {
		return nil, fmt.Errorf("failed to apply auth headers: %w", err)
	}

	// Log request if verbose
	if c.verbose {
		c.logRequest(req)
	}

	// Send request with retries
	var resp *http.Response
	var lastErr error

	for attempt := 1; attempt <= MaxRetryAttempts; attempt++ {
		resp, err = c.httpClient.Do(req)
		if err == nil {
			break // Success
		}

		lastErr = err
		// Only retry on network errors, not on request creation errors
		if attempt < MaxRetryAttempts {
			if c.verbose {
				log.Printf("Request attempt %d failed: %v. Retrying...", attempt, err)
			}
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetworkError, lastErr)
	}

	// Log response if verbose
	if c.verbose {
		c.logResponse(resp)
	}

	// Check for common error responses
	result := &ResponseResult{
		Response: resp,
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		// Success - no error
	case http.StatusUnauthorized:
		resp.Body.Close()
		result.Error = ErrAuthenticationFailed
	case http.StatusNotFound:
		result.Error = ErrResourceNotFound
	case http.StatusBadRequest:
		result.Error = ErrBadRequest
	case http.StatusInternalServerError:
		result.Error = ErrServerError
	default:
		result.Error = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return result, nil
}

// logRequest logs non-sensitive information about the request
func (c *Client) logRequest(req *http.Request) {
	log.Printf("Request: %s %s", req.Method, req.URL.String())
	log.Printf("Headers:")
	for name, values := range req.Header {
		// Don't log the full authorization token for security reasons
		if strings.ToLower(name) == "authorization" {
			log.Printf("  %s: Bearer [TOKEN REDACTED]", name)
		} else {
			log.Printf("  %s: %s", name, values)
		}
	}
	if req.Body != nil {
		log.Printf("Body: [CONTENT NOT LOGGED]")
	}
}

// logResponse logs information about the response
func (c *Client) logResponse(resp *http.Response) {
	log.Printf("Response: %d %s", resp.StatusCode, resp.Status)
	log.Printf("Headers: %v", resp.Header)
}

// ReadResponseBody reads and closes the response body
func ReadResponseBody(result *ResponseResult) ([]byte, error) {
	if result.Error != nil {
		return nil, result.Error
	}

	defer result.Response.Body.Close()

	body, err := io.ReadAll(result.Response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, nil
}

// ===== API ENDPOINT METHODS =====

// FetchRefs retrieves all references from the remote repository
func (c *Client) FetchRefs() (map[string]string, error) {
	result, err := c.sendRequest("GET", EndpointRefs, nil)
	if err != nil {
		return nil, err
	}

	if result.Error != nil {
		return nil, result.Error
	}

	defer result.Response.Body.Close()

	// Try to decode as a direct map first
	body, err := io.ReadAll(result.Response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var refs map[string]string

	// Try to decode as a direct map
	if err := json.Unmarshal(body, &refs); err != nil {
		// If that fails, try to decode as a wrapped object with a "refs" field
		var wrappedRefs struct {
			Refs map[string]string `json:"refs"`
		}
		if err := json.Unmarshal(body, &wrappedRefs); err != nil {
			return nil, fmt.Errorf("failed to decode refs: %w", err)
		}
		refs = wrappedRefs.Refs
	}

	return refs, nil
}

// GetBranchCommit retrieves the commit hash for a specific branch
func (c *Client) GetBranchCommit(branchName string) (string, error) {
	result, err := c.sendRequest("GET", EndpointBranches, nil, branchName)
	if err != nil {
		return "", err
	}

	if result.Error != nil {
		if result.Error == ErrResourceNotFound {
			return "", fmt.Errorf("branch %s not found on remote", branchName)
		}
		return "", result.Error
	}

	defer result.Response.Body.Close()

	var response struct {
		Commit string `json:"commit"`
	}

	if err := json.NewDecoder(result.Response.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Commit, nil
}

// Negotiate determines which objects are missing by negotiating with the server
func (c *Client) Negotiate(remoteRefs, localRefs map[string]string) ([]string, error) {
	negotiationData := map[string]interface{}{
		"want": remoteRefs,
		"have": localRefs,
	}

	result, err := c.sendRequest("POST", EndpointNegotiations, negotiationData)
	if err != nil {
		return nil, err
	}

	if result.Error != nil {
		return nil, result.Error
	}

	defer result.Response.Body.Close()

	var missingObjects []string
	if err := json.NewDecoder(result.Response.Body).Decode(&missingObjects); err != nil {
		return nil, fmt.Errorf("failed to decode missing objects: %w", err)
	}

	return missingObjects, nil
}

// FetchPackfile retrieves a packfile containing the specified objects
func (c *Client) FetchPackfile(objectsList []string) ([]byte, error) {
	result, err := c.sendRequest("POST", EndpointPackfiles, objectsList)
	if err != nil {
		return nil, err
	}

	if result.Error != nil {
		return nil, result.Error
	}

	defer result.Response.Body.Close()

	packfile, err := io.ReadAll(result.Response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read packfile: %w", err)
	}

	return packfile, nil
}

// PushResult contains the result of a push operation
type PushResult struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Errors  []string `json:"errors,omitempty"`
}

// Push sends a packfile and updates refs on the remote repository
func (c *Client) Push(pushData map[string]interface{}, packfile []byte) (*PushResult, error) {
	// Include packfile in push data
	pushData["packfile"] = base64.StdEncoding.EncodeToString(packfile)

	result, err := c.sendRequest("POST", EndpointPushes, pushData)
	if err != nil {
		return nil, err
	}

	defer result.Response.Body.Close()

	var pushResult PushResult

	// Handle non-success status codes that weren't caught as errors
	if result.Response.StatusCode != http.StatusOK && result.Response.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(result.Response.Body)
		pushResult.Success = false
		pushResult.Message = fmt.Sprintf("Server returned status %d: %s", result.Response.StatusCode, string(bodyBytes))
		return &pushResult, nil
	}

	// Parse expected success response
	if err := json.NewDecoder(result.Response.Body).Decode(&pushResult); err != nil {
		return nil, fmt.Errorf("failed to decode push response: %w", err)
	}

	return &pushResult, nil
}

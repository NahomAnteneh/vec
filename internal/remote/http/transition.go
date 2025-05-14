// Package http provides transition functions for migrating to centralized HTTP client
package http

import (
	"net/http"

	"github.com/NahomAnteneh/vec/internal/config"
)

// GetClient returns a configured HTTP client for a particular remote
// This is the main entry point for using the centralized HTTP client
func GetClient(remoteURL, remoteName string, cfg *config.Config) *Client {
	return NewClient(remoteURL, remoteName, cfg)
}

// MakeRemoteRequest is a drop-in replacement for the old makeRemoteRequest function
// It provides identical functionality but uses the new centralized client
func MakeRemoteRequest(remoteURL, endpoint, method string, data interface{}, cfg *config.Config, remoteName string) (*http.Response, error) {
	// Create an HTTP client using the new centralized implementation
	client := NewClient(remoteURL, remoteName, cfg)

	// Set verbose mode based on environment or config
	client.SetVerbose(false) // Default to false, could be configurable

	// Make the request using the new client
	result, err := client.sendRequest(method, endpoint, data)
	if err != nil {
		return nil, err
	}

	// Return just the response for compatibility with the old function
	// The caller will need to close the body as before
	return result.Response, result.Error
}

// FetchRemoteRefs is a transitional replacement for the fetchRemoteRefs function
func FetchRemoteRefs(remoteURL, remoteName string, cfg *config.Config) (map[string]string, error) {
	client := NewClient(remoteURL, remoteName, cfg)

	// Use the centralized client to fetch refs
	return client.FetchRefs()
}

// NegotiateFetch is a transitional replacement for the negotiateFetch function
func NegotiateFetch(remoteURL, remoteName string, remoteRefs, localRefs map[string]string, cfg *config.Config) ([]string, error) {
	client := NewClient(remoteURL, remoteName, cfg)

	// Use the centralized client to negotiate
	return client.Negotiate(remoteRefs, localRefs)
}

// FetchPackfile is a transitional replacement for the fetchPackfile function
func FetchPackfile(remoteURL, remoteName string, objectsList []string, cfg *config.Config) ([]byte, error) {
	client := NewClient(remoteURL, remoteName, cfg)

	// Use the centralized client to fetch packfile
	return client.FetchPackfile(objectsList)
}

// GetBranchCommit is a transitional replacement for getRemoteBranchCommitForPull
func GetBranchCommit(remoteURL, remoteName, branchName string, cfg *config.Config) (string, error) {
	client := NewClient(remoteURL, remoteName, cfg)

	// Use the centralized client to get branch commit
	return client.GetBranchCommit(branchName)
}

// PerformPush is a transitional replacement for performPush
func PerformPush(remoteURL, remoteName string, pushData map[string]interface{}, packfile []byte, cfg *config.Config) (*PushResult, error) {
	client := NewClient(remoteURL, remoteName, cfg)

	// Use the centralized client to perform push
	return client.Push(pushData, packfile)
}

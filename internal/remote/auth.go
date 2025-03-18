package remote

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/internal/config"
)

// ApplyAuthHeaders adds appropriate authentication headers to HTTP requests
func ApplyAuthHeaders(req *http.Request, remoteName string, cfg *config.Config) error {
	log.Printf("[ApplyAuthHeaders] Adding auth headers for remote '%s'", remoteName)

	var token string
	var foundToken bool

	// First check if we have a token in the config
	if cfg != nil {
		log.Printf("[ApplyAuthHeaders] Checking config for auth token")
		configToken, configErr := cfg.GetRemoteAuth(remoteName)
		if configErr != nil {
			log.Printf("[ApplyAuthHeaders] Error getting auth from config: %v", configErr)
		} else if configToken != "" {
			log.Printf("[ApplyAuthHeaders] Found token in config for remote '%s'", remoteName)
			token = configToken
			foundToken = true
		} else {
			log.Printf("[ApplyAuthHeaders] No token found in config for remote '%s'", remoteName)
		}
	} else {
		log.Printf("[ApplyAuthHeaders] Config is nil, cannot check for auth token")
	}

	// If no token in config, try credentials file
	if !foundToken {
		credToken, err := getAuthToken(remoteName)
		if err != nil {
			log.Printf("[ApplyAuthHeaders] Warning: Could not get auth token from credentials for remote '%s': %v", remoteName, err)
		} else if credToken != "" {
			log.Printf("[ApplyAuthHeaders] Found token in credentials file for remote '%s'", remoteName)
			token = credToken
			foundToken = true
		}
	}

	if token != "" {
		log.Printf("[ApplyAuthHeaders] Adding JWT authorization header for remote '%s'", remoteName)
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	} else {
		log.Printf("[ApplyAuthHeaders] No authentication token found for remote '%s'", remoteName)
	}

	// Add any custom headers from config
	if cfg != nil {
		headers, err := cfg.GetRemoteHeaders(remoteName)
		if err == nil && headers != nil {
			for key, value := range headers {
				req.Header.Set(key, value)
			}
		}
	}

	// Log the headers we're sending
	log.Printf("[ApplyAuthHeaders] Final headers:")
	for name, values := range req.Header {
		if strings.ToLower(name) == "authorization" {
			log.Printf("[ApplyAuthHeaders]   %s: Bearer [TOKEN REDACTED]", name)
		} else {
			log.Printf("[ApplyAuthHeaders]   %s: %s", name, values)
		}
	}

	return nil
}

// getAuthToken retrieves the authentication token for the specified remote
func getAuthToken(remoteName string) (string, error) {
	// Get the vec config directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Look for the token in ~/.vec/credentials or a similar location
	credsPath := filepath.Join(homeDir, ".vec", "credentials")
	if _, err := os.Stat(credsPath); os.IsNotExist(err) {
		return "", nil // No credentials file exists yet
	}

	credsData, err := os.ReadFile(credsPath)
	if err != nil {
		return "", fmt.Errorf("failed to read credentials file: %w", err)
	}

	// Parse the credentials file (simple format for now)
	lines := strings.Split(string(credsData), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Skip empty lines and comments
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue // Invalid format
		}

		remote := strings.TrimSpace(parts[0])
		token := strings.TrimSpace(parts[1])

		if remote == remoteName {
			return token, nil
		}
	}

	return "", nil // No token found for this remote
}

// GetAuthToken is an exported version of getAuthToken that retrieves
// the authentication token for the specified remote
func GetAuthToken(remoteName string) (string, error) {
	return getAuthToken(remoteName)
}

// StoreAuthToken saves an authentication token for a remote
func StoreAuthToken(remoteName, token string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	vecDir := filepath.Join(homeDir, ".vec")
	if err := os.MkdirAll(vecDir, 0755); err != nil {
		return fmt.Errorf("failed to create .vec directory: %w", err)
	}

	credsPath := filepath.Join(vecDir, "credentials")

	// Read existing credentials if available
	existingCreds := make(map[string]string)
	if _, err := os.Stat(credsPath); err == nil {
		credsData, err := os.ReadFile(credsPath)
		if err != nil {
			return fmt.Errorf("failed to read credentials file: %w", err)
		}

		lines := strings.Split(string(credsData), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}

			existingCreds[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	// Update or add the token for this remote
	existingCreds[remoteName] = token

	// Write back to the file
	var content strings.Builder
	content.WriteString("# Vec credentials file - DO NOT SHARE\n")
	content.WriteString("# Format: remote_name=token\n")

	for remote, tok := range existingCreds {
		content.WriteString(fmt.Sprintf("%s=%s\n", remote, tok))
	}

	if err := os.WriteFile(credsPath, []byte(content.String()), 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	return nil
}

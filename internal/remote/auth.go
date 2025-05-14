package remote

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/internal/config"
	vechttp "github.com/NahomAnteneh/vec/internal/remote/http"
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

// LoginToRemote performs an interactive login to a remote repository
// and stores the authentication token in the configuration
func LoginToRemote(remoteName string, username string, password string) error {
	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Load config
	cfg, err := config.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	remoteURL, err := GetRemoteURL(remoteName)
	if err != nil {
		return fmt.Errorf("failed to get remote URL: %w", err)
	}

	// Create a new HTTP client
	client := vechttp.NewClient(remoteURL, remoteName, cfg)

	// Perform login
	token, refreshToken, err := client.Login(username, password)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Store the tokens
	if err := StoreAuthToken(remoteName, token); err != nil {
		return fmt.Errorf("failed to store auth token: %w", err)
	}

	if err := StoreRefreshToken(remoteName, refreshToken); err != nil {
		return fmt.Errorf("failed to store refresh token: %w", err)
	}

	return nil
}

// StoreRefreshToken saves a refresh token for a remote
func StoreRefreshToken(remoteName, token string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	vecDir := filepath.Join(homeDir, ".vec")
	if err := os.MkdirAll(vecDir, 0755); err != nil {
		return fmt.Errorf("failed to create .vec directory: %w", err)
	}

	refreshTokensPath := filepath.Join(vecDir, "refresh_tokens")

	// Read existing tokens if available
	existingTokens := make(map[string]string)
	if _, err := os.Stat(refreshTokensPath); err == nil {
		tokensData, err := os.ReadFile(refreshTokensPath)
		if err != nil {
			return fmt.Errorf("failed to read refresh tokens file: %w", err)
		}

		lines := strings.Split(string(tokensData), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}

			existingTokens[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	// Update or add the token for this remote
	existingTokens[remoteName] = token

	// Write back to the file
	var content strings.Builder
	content.WriteString("# Vec refresh tokens - DO NOT SHARE\n")
	content.WriteString("# Format: remote_name=token\n")

	for remote, tok := range existingTokens {
		content.WriteString(fmt.Sprintf("%s=%s\n", remote, tok))
	}

	if err := os.WriteFile(refreshTokensPath, []byte(content.String()), 0600); err != nil {
		return fmt.Errorf("failed to write refresh tokens file: %w", err)
	}

	return nil
}

// GetRefreshToken retrieves the refresh token for the specified remote
func GetRefreshToken(remoteName string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	refreshTokensPath := filepath.Join(homeDir, ".vec", "refresh_tokens")
	if _, err := os.Stat(refreshTokensPath); os.IsNotExist(err) {
		return "", nil // No tokens file exists yet
	}

	tokensData, err := os.ReadFile(refreshTokensPath)
	if err != nil {
		return "", fmt.Errorf("failed to read refresh tokens file: %w", err)
	}

	// Parse the tokens file
	lines := strings.Split(string(tokensData), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		remote := strings.TrimSpace(parts[0])
		token := strings.TrimSpace(parts[1])

		if remote == remoteName {
			return token, nil
		}
	}

	return "", nil // No refresh token found for this remote
}

// RefreshAuthToken attempts to refresh an expired authentication token
func RefreshAuthToken(remoteName string) (string, error) {
	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Load config
	cfg, err := config.LoadConfig(cwd)
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	remoteURL, err := GetRemoteURL(remoteName)
	if err != nil {
		return "", fmt.Errorf("failed to get remote URL: %w", err)
	}

	refreshToken, err := GetRefreshToken(remoteName)
	if err != nil {
		return "", fmt.Errorf("failed to get refresh token: %w", err)
	}

	// Create a new HTTP client
	client := vechttp.NewClient(remoteURL, remoteName, cfg)

	// Perform token refresh
	newToken, newRefreshToken, err := client.RefreshToken(refreshToken)
	if err != nil {
		return "", fmt.Errorf("token refresh failed: %w", err)
	}

	// Store the new tokens
	if err := StoreAuthToken(remoteName, newToken); err != nil {
		return "", fmt.Errorf("failed to store new auth token: %w", err)
	}

	if err := StoreRefreshToken(remoteName, newRefreshToken); err != nil {
		return "", fmt.Errorf("failed to store new refresh token: %w", err)
	}

	return newToken, nil
}

// GetRemoteURL retrieves the URL for a given remote name
func GetRemoteURL(remoteName string) (string, error) {
	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Load config from current directory
	cfg, err := config.LoadConfig(cwd)
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

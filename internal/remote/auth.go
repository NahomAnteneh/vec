package remote

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StoreCredentials saves credentials for a remote in the credentials file
func StoreCredentials(remoteName, username, password string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	vecDir := filepath.Join(homeDir, ".vec")
	if err := os.MkdirAll(vecDir, 0755); err != nil {
		return fmt.Errorf("failed to create .vec directory: %w", err)
	}

	credsPath := filepath.Join(vecDir, "credentials")

	// Read existing credentials file
	var lines []string
	if _, err := os.Stat(credsPath); err == nil {
		credsData, err := os.ReadFile(credsPath)
		if err != nil {
			return fmt.Errorf("failed to read credentials file: %w", err)
		}
		lines = strings.Split(string(credsData), "\n")
	} else {
		// Create a new file with header
		lines = []string{
			"# Vec credentials file - DO NOT SHARE",
			"# Format: remote.{name}.username=value",
			"# Format: remote.{name}.password=value",
			"",
		}
	}

	// Update or add credentials
	userFound, passFound := false, false
	for i, line := range lines {
		if strings.HasPrefix(line, fmt.Sprintf("remote.%s.username=", remoteName)) {
			lines[i] = fmt.Sprintf("remote.%s.username=%s", remoteName, username)
			userFound = true
		}
		if strings.HasPrefix(line, fmt.Sprintf("remote.%s.password=", remoteName)) {
			lines[i] = fmt.Sprintf("remote.%s.password=%s", remoteName, password)
			passFound = true
		}
	}

	// Add new entries if not found
	if !userFound {
		lines = append(lines, fmt.Sprintf("remote.%s.username=%s", remoteName, username))
	}
	if !passFound {
		lines = append(lines, fmt.Sprintf("remote.%s.password=%s", remoteName, password))
	}

	// Write back to file
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(credsPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	return nil
}

// ClearCredentials removes credentials for a remote
func ClearCredentials(remoteName string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	credsPath := filepath.Join(homeDir, ".vec", "credentials")
	if _, err := os.Stat(credsPath); os.IsNotExist(err) {
		// No credentials file, nothing to clear
		return nil
	}

	credsData, err := os.ReadFile(credsPath)
	if err != nil {
		return fmt.Errorf("failed to read credentials file: %w", err)
	}

	lines := strings.Split(string(credsData), "\n")
	var newLines []string
	
	// Filter out lines for the specified remote
	for _, line := range lines {
		if !strings.HasPrefix(line, fmt.Sprintf("remote.%s.username=", remoteName)) && 
		   !strings.HasPrefix(line, fmt.Sprintf("remote.%s.password=", remoteName)) {
			newLines = append(newLines, line)
		}
	}

	// Write back to file
	content := strings.Join(newLines, "\n")
	if err := os.WriteFile(credsPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	return nil
}

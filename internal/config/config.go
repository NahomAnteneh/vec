package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/utils"
)

// Remote represents a remote repository entry.
type Remote struct {
	URL          string
	Fetch        string
	Auth         string            // JWT token or other authentication info
	ExtraHeaders map[string]string // Additional HTTP headers
}

// Config holds the repository configuration.
type Config struct {
	Remotes  map[string]Remote            // keyed by remote name (e.g., "origin")
	Settings map[string]map[string]string // additional settings organized by section
	path     string                       // config file path (internal only)
}

// NewConfig creates a new Config instance given a repository root.
func NewConfig(repoRoot string) *Config {
	configPath := filepath.Join(repoRoot, ".vec", "config")
	return &Config{
		Remotes:  make(map[string]Remote),
		Settings: make(map[string]map[string]string),
		path:     configPath,
	}
}

// Load reads the config file from disk (or returns a new config if none exists).
func LoadConfig(repoRoot string) (*Config, error) {
	cfg := NewConfig(repoRoot)
	if !utils.FileExists(cfg.path) {
		return cfg, nil
	}

	data, err := os.ReadFile(cfg.path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var currentSection string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.TrimSpace(line[1 : len(line)-1])
			if strings.HasPrefix(currentSection, "remote \"") {
				remoteName := strings.Trim(currentSection[7:], "\"")
				cfg.Remotes[remoteName] = Remote{
					ExtraHeaders: make(map[string]string),
				}
			} else {
				if _, exists := cfg.Settings[currentSection]; !exists {
					cfg.Settings[currentSection] = make(map[string]string)
				}
			}
			continue
		}
		if currentSection == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if strings.HasPrefix(currentSection, "remote \"") {
			remoteName := strings.Trim(currentSection[7:], "\"")
			remote := cfg.Remotes[remoteName]
			switch key {
			case "url":
				remote.URL = value
			case "fetch":
				remote.Fetch = value
			case "auth":
				remote.Auth = value
			}
			// Handle custom headers with the prefix "header."
			if strings.HasPrefix(key, "header.") {
				headerName := strings.TrimPrefix(key, "header.")
				if remote.ExtraHeaders == nil {
					remote.ExtraHeaders = make(map[string]string)
				}
				remote.ExtraHeaders[headerName] = value
			}
			cfg.Remotes[remoteName] = remote
		} else {
			cfg.Settings[currentSection][key] = value
		}
	}
	return cfg, nil
}

// Write saves the configuration to disk.
func (c *Config) Write() error {
	var buf strings.Builder
	for section, keys := range c.Settings {
		buf.WriteString(fmt.Sprintf("[%s]\n", section))
		for key, value := range keys {
			buf.WriteString(fmt.Sprintf("    %s = %s\n", key, value))
		}
	}
	for name, remote := range c.Remotes {
		buf.WriteString(fmt.Sprintf("[remote \"%s\"]\n", name))
		buf.WriteString(fmt.Sprintf("    url = %s\n", remote.URL))
		buf.WriteString(fmt.Sprintf("    fetch = %s\n", remote.Fetch))
		if remote.Auth != "" {
			buf.WriteString(fmt.Sprintf("    auth = %s\n", remote.Auth))
		}
		// Write any extra headers
		for headerName, headerValue := range remote.ExtraHeaders {
			buf.WriteString(fmt.Sprintf("    header.%s = %s\n", headerName, headerValue))
		}
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.WriteFile(c.path, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// GetRemoteURL retrieves the URL for the specified remote.
func (c *Config) GetRemoteURL(name string) (string, error) {
	if remote, exists := c.Remotes[name]; exists {
		return remote.URL, nil
	}
	return "", fmt.Errorf("remote '%s' not found", name)
}

// AddRemote adds or updates a remote with the provided URL and a default fetch refspec.
func (c *Config) AddRemote(name, url string) error {
	if name == "" || url == "" {
		return fmt.Errorf("remote name and URL cannot be empty")
	}
	remote, exists := c.Remotes[name]
	if !exists {
		remote = Remote{
			ExtraHeaders: make(map[string]string),
		}
	}
	remote.URL = url
	remote.Fetch = fmt.Sprintf("+refs/heads/*:refs/remotes/%s/*", name)
	c.Remotes[name] = remote
	return nil
}

// RemoveRemote deletes the given remote from the configuration.
func (c *Config) RemoveRemote(name string) error {
	if _, exists := c.Remotes[name]; !exists {
		return fmt.Errorf("remote '%s' does not exist", name)
	}
	delete(c.Remotes, name)
	return nil
}

// RenameRemote renames an existing remote.
func (c *Config) RenameRemote(oldName, newName string) error {
	if oldName == "" || newName == "" {
		return fmt.Errorf("remote names cannot be empty")
	}
	if _, exists := c.Remotes[oldName]; !exists {
		return fmt.Errorf("remote '%s' does not exist", oldName)
	}
	if _, exists := c.Remotes[newName]; exists {
		return fmt.Errorf("remote '%s' already exists", newName)
	}
	c.Remotes[newName] = c.Remotes[oldName]
	delete(c.Remotes, oldName)
	return nil
}

// GetDefaultRemote returns the name and URL of the default remote.
// If 'origin' is present, that is returned; otherwise, the first available remote is used.
func (c *Config) GetDefaultRemote() (string, string, error) {
	if remote, exists := c.Remotes["origin"]; exists {
		return "origin", remote.URL, nil
	}
	for name, remote := range c.Remotes {
		return name, remote.URL, nil
	}
	return "", "", fmt.Errorf("no remotes configured")
}

// SetRemoteAuth sets the authentication token for a remote
func (c *Config) SetRemoteAuth(remoteName, authToken string) error {
	remote, exists := c.Remotes[remoteName]
	if !exists {
		return fmt.Errorf("remote '%s' does not exist", remoteName)
	}
	remote.Auth = authToken
	c.Remotes[remoteName] = remote
	return nil
}

// GetRemoteAuth gets the authentication token for a remote
func (c *Config) GetRemoteAuth(remoteName string) (string, error) {
	remote, exists := c.Remotes[remoteName]
	if !exists {
		return "", fmt.Errorf("remote '%s' does not exist", remoteName)
	}
	return remote.Auth, nil
}

// SetRemoteHeader sets a custom HTTP header for a remote
func (c *Config) SetRemoteHeader(remoteName, headerName, headerValue string) error {
	remote, exists := c.Remotes[remoteName]
	if !exists {
		return fmt.Errorf("remote '%s' does not exist", remoteName)
	}
	if remote.ExtraHeaders == nil {
		remote.ExtraHeaders = make(map[string]string)
	}
	remote.ExtraHeaders[headerName] = headerValue
	c.Remotes[remoteName] = remote
	return nil
}

// GetRemoteHeaders gets all custom HTTP headers for a remote
func (c *Config) GetRemoteHeaders(remoteName string) (map[string]string, error) {
	remote, exists := c.Remotes[remoteName]
	if !exists {
		return nil, fmt.Errorf("remote '%s' does not exist", remoteName)
	}
	if remote.ExtraHeaders == nil {
		return make(map[string]string), nil
	}
	return remote.ExtraHeaders, nil
}

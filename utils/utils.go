package utils

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Common constants
const (
	VecDirName = ".vec"
	HeadFile   = "HEAD"
)

// Global cache for ignore patterns to avoid reloading and reparsing .vecignore
var (
	ignorePatternCache      = make(map[string][]string)
	ignorePatternCacheMutex sync.RWMutex
)

// FileExists checks if a file exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// ReadFileContent reads the content of a file.
func ReadFileContent(filePath string) ([]byte, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return content, nil
}

// HashFile calculates the SHA-256 hash of a file, including the Vec object header.
func HashFile(filePath string) (string, error) {
	content, err := ReadFileContent(filePath)
	if err != nil {
		return "", err
	}
	return HashBytes("blob", content), nil
}

// HashBytes calculates the SHA-256 hash of the given data, including the Vec object header.
func HashBytes(objectType string, data []byte) string {
	header := fmt.Sprintf("%s %d\n", objectType, len(data))
	h := sha256.New()
	h.Write([]byte(header))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// EnsureDirExists creates a directory if it doesn't exist.
func EnsureDirExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", path, err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to stat directory %s: %w", path, err)
	}
	return nil
}

// ReadHEADFile reads the content of the HEAD file and returns it trimmed.
func ReadHEADFile(repoRoot string) (string, error) {
	headPath := filepath.Join(repoRoot, VecDirName, HeadFile)
	headContent, err := ReadFileContent(headPath)
	if err != nil {
		return "", fmt.Errorf("failed to read HEAD: %w", err)
	}
	return strings.TrimSpace(string(headContent)), nil
}

// ReadHEAD retrieves the commit ID that HEAD points to.
func ReadHEAD(repoRoot string) (string, error) {
	headContent, err := ReadHEADFile(repoRoot)
	if err != nil {
		return "", err
	}

	// Check if HEAD is a ref
	if strings.HasPrefix(headContent, "ref: ") {
		refPath := strings.TrimPrefix(headContent, "ref: ")
		refFile := filepath.Join(repoRoot, VecDirName, refPath)

		// Check if reference file exists
		if !FileExists(refFile) {
			return "", nil // No commits yet
		}

		commitID, err := ReadFileContent(refFile)
		if err != nil {
			return "", fmt.Errorf("failed to read reference file '%s': %w", refPath, err)
		}
		return strings.TrimSpace(string(commitID)), nil
	}

	// Handle detached HEAD (direct commit hash)
	if len(headContent) == 64 && IsValidHex(headContent) {
		return headContent, nil
	}

	return "", fmt.Errorf("invalid HEAD content: %s", headContent)
}

// GetHeadCommit gets the SHA-256 of the current HEAD commit.
// This is an alias for ReadHEAD for backward compatibility.
func GetHeadCommit(repoRoot string) (string, error) {
	return ReadHEAD(repoRoot)
}

// GetCurrentBranch returns the name of the current branch.
func GetCurrentBranch(repoRoot string) (string, error) {
	headContent, err := ReadHEADFile(repoRoot)
	if err != nil {
		return "", err
	}

	// Check if HEAD is a ref
	if strings.HasPrefix(headContent, "ref: ") {
		refPath := strings.TrimPrefix(headContent, "ref: ")
		parts := strings.Split(refPath, "/")
		branchName := parts[len(parts)-1] // Get the last part
		return branchName, nil
	}

	// Detached HEAD
	return "(HEAD detached)", nil
}

// IsValidHex checks if a string is a valid hexadecimal value.
func IsValidHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// IsIgnored checks if a given path should be ignored by Vec.
func IsIgnored(repoRoot, path string) (bool, error) {
	// First ensure we're working with absolute paths
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("failed to get absolute path: %w", err)
	}

	absRepoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return false, fmt.Errorf("failed to get absolute repo root path: %w", err)
	}

	// Get path relative to repository root
	relPath, err := filepath.Rel(absRepoRoot, absPath)
	if err != nil {
		return false, fmt.Errorf("failed to get relative path: %w", err)
	}

	// Ignore .vec directory and its contents
	if strings.HasPrefix(relPath, VecDirName) {
		return true, nil
	}

	// Check for cached patterns first
	ignorePatternCacheMutex.RLock()
	patterns, ok := ignorePatternCache[absRepoRoot]
	ignorePatternCacheMutex.RUnlock()

	// If not in cache, load patterns from .vecignore
	if !ok {
		patterns = loadIgnorePatterns(absRepoRoot)
	}

	// Match against patterns
	return matchIgnorePatterns(patterns, relPath), nil
}

// loadIgnorePatterns loads and caches patterns from .vecignore file
func loadIgnorePatterns(absRepoRoot string) []string {
	vecignorePath := filepath.Join(absRepoRoot, ".vecignore")
	patterns := []string{}

	if FileExists(vecignorePath) {
		vecignoreContent, err := ReadFileContent(vecignorePath)
		if err == nil {
			// Parse valid patterns
			rawPatterns := strings.Split(string(vecignoreContent), "\n")
			patterns = make([]string, 0, len(rawPatterns))

			for _, pattern := range rawPatterns {
				pattern = strings.TrimSpace(pattern)
				if pattern == "" || strings.HasPrefix(pattern, "#") {
					continue // Skip empty lines and comments
				}

				// Validate pattern before adding to cache
				if _, err := filepath.Match(pattern, "test-filename"); err != nil {
					// Log invalid pattern but don't fail
					fmt.Fprintf(os.Stderr, "warning: invalid pattern in .vecignore: %s\n", pattern)
					continue
				}

				patterns = append(patterns, filepath.Clean(pattern))
			}
		}
	}

	// Cache the parsed patterns
	ignorePatternCacheMutex.Lock()
	ignorePatternCache[absRepoRoot] = patterns
	ignorePatternCacheMutex.Unlock()

	return patterns
}

// matchIgnorePatterns checks if a path matches any ignore patterns
func matchIgnorePatterns(patterns []string, relPath string) bool {
	for _, pattern := range patterns {
		// Check for direct match first
		matched, _ := filepath.Match(pattern, relPath) // Error already checked during parsing
		if matched {
			return true
		}

		// Check if pattern matches any parent directory
		// Split path only once and reuse for all patterns
		relPathParts := strings.Split(relPath, string(filepath.Separator))
		for i := range relPathParts {
			partialPath := filepath.Join(relPathParts[:i+1]...)
			if matched, _ := filepath.Match(pattern, partialPath); matched {
				return true
			}
		}
	}

	return false
}

// GetVecRoot returns the root directory of the Vec repository.
// It searches for the .vec directory in the current and parent directories.
func GetVecRoot() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Store the original starting point to help with error message
	startDir := currentDir

	// Check for environment variable to force a specific repository path
	if forcedRoot := os.Getenv("VEC_REPOSITORY_PATH"); forcedRoot != "" {
		vecDir := filepath.Join(forcedRoot, VecDirName)
		if FileExists(vecDir) {
			return forcedRoot, nil
		}
		return "", fmt.Errorf("VEC_REPOSITORY_PATH is set to '%s' but no repository found there", forcedRoot)
	}

	// Search up the directory tree for a .vec directory
	for {
		vecDir := filepath.Join(currentDir, VecDirName)
		if FileExists(vecDir) {
			return currentDir, nil
		}

		// Move to the parent directory
		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir { // Reached root
			return "", fmt.Errorf("not a vec repository (or any of the parent directories): %s", startDir)
		}
		currentDir = parentDir
	}
}

// CopyFile copies a file from src to dst.
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return fmt.Errorf("failed to copy file content: %w", err)
	}

	return nil
}

// GetGlobalConfigPath returns the path to the global configuration file.
func GetGlobalConfigPath() (string, error) {
	var homeDir string
	if runtime.GOOS == "windows" {
		homeDir = os.Getenv("USERPROFILE")
	} else {
		homeDir = os.Getenv("HOME")
	}

	if homeDir == "" {
		return "", fmt.Errorf("home directory not found")
	}
	return filepath.Join(homeDir, ".vecconfig"), nil
}

// ReadGlobalConfig reads the global configuration file.
func ReadGlobalConfig() (map[string]string, error) {
	configPath, err := GetGlobalConfigPath()
	if err != nil {
		return nil, err
	}
	return ReadConfig(configPath)
}

// WriteGlobalConfig writes the global configuration file.
func WriteGlobalConfig(config map[string]string) error {
	configPath, err := GetGlobalConfigPath()
	if err != nil {
		return err
	}
	return WriteConfig(configPath, config)
}

// ReadConfig reads a config file (either global or local).
func ReadConfig(filePath string) (map[string]string, error) {
	config := make(map[string]string)

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil // Return empty map if not exist
		}
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Skip empty and comment lines
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key != "" && value != "" { // Prevent empty key/value
			config[key] = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	return config, nil
}

// WriteConfig writes to a config file (either global or local)
func WriteConfig(filePath string, config map[string]string) error {
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for key, value := range config {
		_, err := fmt.Fprintf(writer, "%s = %s\n", key, value)
		if err != nil {
			return fmt.Errorf("failed to write to config file: %w", err)
		}
	}
	return writer.Flush()
}

// GetConfigValue gets a config value, checking local then global.
func GetConfigValue(repoRoot string, key string) (string, error) {
	// First, try to get the local config value.
	localConfig, err := ReadConfig(filepath.Join(repoRoot, VecDirName, "config"))
	if err != nil {
		return "", err
	}
	if value, ok := localConfig[key]; ok {
		return value, nil // Found in local config.
	}

	// If not found locally, try the global config
	globalConfig, err := ReadGlobalConfig()
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if value, ok := globalConfig[key]; ok {
		return value, nil // Found in global config.
	}

	// If not found in either, return an empty string and no error (it's optional).
	return "", nil
}

// getConfigPathAndData gets the config path and data for a specific config type
func getConfigPathAndData(repoRoot string, global bool) (string, map[string]string, error) {
	var configPath string
	var config map[string]string
	var err error

	if global {
		configPath, err = GetGlobalConfigPath()
		if err != nil {
			return "", nil, err
		}
		config, err = ReadGlobalConfig()
	} else {
		configPath = filepath.Join(repoRoot, VecDirName, "config")
		config, err = ReadConfig(configPath)
	}

	if err != nil && !os.IsNotExist(err) {
		return "", nil, err
	} else if os.IsNotExist(err) {
		config = make(map[string]string)
	}

	return configPath, config, nil
}

// SetConfigValue sets a config value (either local or global).
func SetConfigValue(repoRoot string, key string, value string, global bool) error {
	configPath, config, err := getConfigPathAndData(repoRoot, global)
	if err != nil {
		return err
	}

	config[key] = value // Set new value

	if global {
		return WriteGlobalConfig(config)
	}
	return WriteConfig(configPath, config)
}

// UnsetConfigValue unsets (removes) a config value (either local or global).
func UnsetConfigValue(repoRoot string, key string, global bool) error {
	configPath, config, err := getConfigPathAndData(repoRoot, global)
	if err != nil {
		return err
	}

	if _, ok := config[key]; !ok {
		return fmt.Errorf("config key '%s' not found", key)
	}

	delete(config, key)

	if global {
		return WriteGlobalConfig(config)
	}
	return WriteConfig(configPath, config)
}

// GetAllBranches returns a list of all branches in the repository
func GetAllBranches(repoRoot string) ([]string, error) {
	branchesDir := filepath.Join(repoRoot, VecDirName, "refs", "heads")
	if !FileExists(branchesDir) {
		return nil, fmt.Errorf("branches directory not found")
	}

	var branches []string
	err := filepath.Walk(branchesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip directories, we only want the branch files
		if !info.IsDir() {
			// Get the branch name from the path
			relativePath, err := filepath.Rel(branchesDir, path)
			if err != nil {
				return err
			}
			branches = append(branches, relativePath)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	return branches, nil
}

// SetBranchUpstream sets the upstream branch for a local branch
func SetBranchUpstream(repoRoot, branchName, remoteName string) error {
	// Ensure the branch exists
	branchPath := filepath.Join(repoRoot, VecDirName, "refs", "heads", branchName)
	if !FileExists(branchPath) {
		return fmt.Errorf("branch '%s' does not exist", branchName)
	}

	// Ensure the remote exists in the config
	cfg, err := ReadConfig(filepath.Join(repoRoot, VecDirName, "config"))
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	// Check if remote exists
	remoteUrlKey := fmt.Sprintf("remote.%s.url", remoteName)
	if _, exists := cfg[remoteUrlKey]; !exists {
		return fmt.Errorf("remote '%s' does not exist", remoteName)
	}

	// Set upstream configuration
	branchKey := fmt.Sprintf("branch.%s.remote", branchName)
	mergeKey := fmt.Sprintf("branch.%s.merge", branchName)

	cfg[branchKey] = remoteName
	cfg[mergeKey] = fmt.Sprintf("refs/heads/%s", branchName)

	// Write the updated config
	configPath := filepath.Join(repoRoot, VecDirName, "config")
	return WriteConfig(configPath, cfg)
}

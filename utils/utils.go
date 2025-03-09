// utils/utils.go
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
)

// ... (FileExists, HashFile, HashBytes, EnsureDirExists remain unchanged) ..
// GetHeadCommit gets the SHA-256 of the current HEAD commit (placeholder).
func GetHeadCommit(repoRoot string) (string, error) {
	headPath := filepath.Join(repoRoot, ".vec", "HEAD")
	headContent, err := os.ReadFile(headPath)
	if err != nil {
		return "", fmt.Errorf("failed to read HEAD: %w", err)
	}

	headStr := strings.TrimSpace(string(headContent))

	// Check if HEAD is a ref
	if strings.HasPrefix(headStr, "ref: ") {
		refPath := strings.TrimPrefix(headStr, "ref: ")
		branchPath := filepath.Join(repoRoot, ".vec", refPath)

		// Check if branch file exists. If not, there's no commit yet.
		if !FileExists(branchPath) {
			return "", nil // No commits yet.
		}

		commitID, err := os.ReadFile(branchPath)
		if err != nil {
			return "", fmt.Errorf("failed to read branch file: %w", err)
		}
		return strings.TrimSpace(string(commitID)), nil
	} else { // Detached HEAD
		return headStr, nil
	}
}

func GetCurrentBranch(repoRoot string) (string, error) {
	headPath := filepath.Join(repoRoot, ".vec", "HEAD")
	headContent, err := os.ReadFile(headPath)
	if err != nil {
		return "", fmt.Errorf("failed to read HEAD: %w", err)
	}

	headStr := strings.TrimSpace(string(headContent))

	// Check if HEAD is a ref
	if strings.HasPrefix(headStr, "ref: ") {
		refPath := strings.TrimPrefix(headStr, "ref: ")
		parts := strings.Split(refPath, "/")
		branchName := parts[len(parts)-1] // Get the last part
		return branchName, nil
	} else { // Detached HEAD
		return "(HEAD detached)", nil
	}
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
	if strings.HasPrefix(relPath, ".vec") {
		return true, nil
	}

	// Check for .vecignore file
	vecignorePath := filepath.Join(absRepoRoot, ".vecignore")
	if FileExists(vecignorePath) {
		vecignoreContent, err := os.ReadFile(vecignorePath)
		if err != nil {
			return false, fmt.Errorf("failed to read .vecignore: %w", err)
		}

		patterns := strings.Split(string(vecignoreContent), "\n")
		for _, pattern := range patterns {
			pattern = strings.TrimSpace(pattern)
			if pattern == "" || strings.HasPrefix(pattern, "#") {
				continue // Skip empty lines and comments
			}

			// Convert the pattern to a filepath pattern
			pattern = filepath.Clean(pattern)

			// Handle both exact matches and glob patterns
			matched, err := filepath.Match(pattern, relPath)
			if err != nil {
				// Skip invalid patterns but continue processing others
				continue
			}
			if matched {
				return true, nil
			}

			// Also check if pattern matches any parent directory
			relPathParts := strings.Split(relPath, string(filepath.Separator))
			for i := range relPathParts {
				partialPath := filepath.Join(relPathParts[:i+1]...)
				matched, err := filepath.Match(pattern, partialPath)
				if err == nil && matched {
					return true, nil
				}
			}
		}
	}

	return false, nil
}

// FileExists checks if a file exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// HashFile calculates the SHA-256 hash of a file, including the Vec object header.
func HashFile(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
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

// GetVecRoot returns the root directory of the Vec repository.
// It searches for the .vec directory in the current and parent directories.
func GetVecRoot() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	for {
		vecDir := filepath.Join(currentDir, ".vec")
		if FileExists(vecDir) {
			return currentDir, nil
		}

		// Move to the parent directory.
		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir { // Reached root.
			return "", fmt.Errorf("not a vec repository (or any of the parent directories)")
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

// ReadFileContent reads the content of a file.
func ReadFileContent(filePath string) ([]byte, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return content, nil
}

// ... (other utility functions remain unchanged) ...
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
	return ReadConfig(configPath) // Reuse the existing ReadConfig function.
}

// WriteGlobalConfig writes the global configuration file.
func WriteGlobalConfig(config map[string]string) error {
	configPath, err := GetGlobalConfigPath()
	if err != nil {
		return err
	}
	return WriteConfig(configPath, config) // Reuse the existing WriteConfig function.
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
	return writer.Flush() // Important!
}

// GetConfigValue gets a config value, checking local then global.
func GetConfigValue(repoRoot string, key string) (string, error) {
	// First, try to get the local config value.
	localConfig, err := ReadConfig(filepath.Join(repoRoot, ".vec", "config"))
	if err != nil {
		return "", err
	}
	if value, ok := localConfig[key]; ok {
		return value, nil // Found in local config.
	}

	// If not found locally, try the global config.
	globalConfig, err := ReadGlobalConfig()
	if err != nil {
		return "", err
	}
	if value, ok := globalConfig[key]; ok {
		return value, nil // Found in global config.
	}

	// If not found in either, return an empty string and no error (it's optional).
	return "", nil
}

// SetConfigValue sets a config value (either local or global).
func SetConfigValue(repoRoot string, key string, value string, global bool) error {
	var config map[string]string
	var err error
	var configPath string

	if global {
		configPath, err = GetGlobalConfigPath()
		if err != nil {
			return err
		}
		config, err = ReadGlobalConfig() // Read global config
	} else {
		configPath = filepath.Join(repoRoot, ".vec", "config")
		config, err = ReadConfig(configPath) // Read local config
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	} else if os.IsNotExist(err) {
		config = make(map[string]string) //If there is not any error and file not exist create one
	}
	config[key] = value // Set new value

	// Write to file
	if global {
		err = WriteGlobalConfig(config)
	} else {
		file, err := os.Create(configPath)
		if err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}
		defer file.Close()
		writer := bufio.NewWriter(file)

		for key, value := range config {
			_, err := fmt.Fprintf(writer, "%s = %s\n", key, value)
			if err != nil {
				return fmt.Errorf("failed to write config file: %w", err)
			}
		}
		err = writer.Flush() // Very important
		if err != nil {
			return fmt.Errorf("failed to flush config file: %w", err)
		}
	}
	return nil
}

// UnsetConfigValue unsets (removes) a config value (either local or global).
func UnsetConfigValue(repoRoot string, key string, global bool) error {
	var config map[string]string
	var err error
	var configPath string
	if global {
		configPath, err = GetGlobalConfigPath()
		if err != nil {
			return err
		}
		config, err = ReadGlobalConfig()
	} else {
		configPath = filepath.Join(repoRoot, ".vec", "config")
		config, err = ReadConfig(configPath)
	}
	if err != nil {
		return err
	}

	if _, ok := config[key]; !ok {
		return fmt.Errorf("config key '%s' not found", key)
	}

	delete(config, key)

	// Write to file
	if global {
		err = WriteGlobalConfig(config)
	} else {
		file, err := os.Create(configPath) // Recreate to write
		if err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}
		defer file.Close()
		writer := bufio.NewWriter(file)

		for key, value := range config { // Write all entries
			_, err := fmt.Fprintf(writer, "%s = %s\n", key, value)
			if err != nil {
				return fmt.Errorf("failed to write config file: %w", err)
			}
		}
		err = writer.Flush() // Very important
		if err != nil {
			return fmt.Errorf("failed to flush config file: %w", err)
		}
	}

	return nil
}

// ReadHEAD retrieves the commit ID that HEAD points to.
func ReadHEAD(repoRoot string) (string, error) {
	headFile := filepath.Join(repoRoot, ".vec", "HEAD")
	content, err := os.ReadFile(headFile)
	if err != nil {
		return "", fmt.Errorf("failed to read HEAD file: %w", err)
	}

	ref := strings.TrimSpace(string(content))
	if strings.HasPrefix(ref, "ref: ") {
		refPath := strings.TrimSpace(ref[5:])
		refFile := filepath.Join(repoRoot, ".vec", refPath)
		commitID, err := os.ReadFile(refFile)
		if err != nil {
			return "", fmt.Errorf("failed to read reference file '%s': %w", refPath, err)
		}
		return strings.TrimSpace(string(commitID)), nil
	}

	if len(ref) == 64 && IsValidHex(ref) { // Assuming SHA-256 (64 chars)
		return ref, nil
	}

	return "", fmt.Errorf("invalid HEAD content: %s", ref)
}

// isValidHex checks if a string is a valid hexadecimal value.
func IsValidHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

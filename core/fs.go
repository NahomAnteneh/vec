package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Common constants
const (
	VecDirName = ".vec"
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

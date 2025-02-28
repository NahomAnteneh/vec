// utils/utils.go
package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	relPath, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return false, fmt.Errorf("failed to get relative path: %w", err)
	}
	// Ignore .vec directory and its contents
	if strings.HasPrefix(relPath, ".vec") {
		return true, nil
	}

	// Check for .gitignore file (basic implementation)
	vecignorePath := filepath.Join(repoRoot, ".vecignore")
	if FileExists(vecignorePath) {
		vecignoreContent, err := os.ReadFile(vecignorePath)
		if err != nil {
			return false, fmt.Errorf("failed to read .gitignore: %w", err)
		}
		patterns := strings.Split(string(vecignoreContent), "\n")
		for _, pattern := range patterns {
			pattern = strings.TrimSpace(pattern)
			if pattern == "" || strings.HasPrefix(pattern, "#") {
				continue // Skip empty lines and comments
			}
			// Convert the gitignore pattern to a glob pattern
			pattern = filepath.Join(repoRoot, pattern)
			matched, err := filepath.Match(pattern, path)
			if err != nil {
				return false, fmt.Errorf("invalid gitignore pattern %s: %w", pattern, err)
			}
			if matched {
				return true, nil
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

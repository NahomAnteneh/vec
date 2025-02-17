package utils

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CopyFile copies a file from source to destination.
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file '%s': %w", src, err)
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file '%s': %w", dst, err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return fmt.Errorf("failed to copy file content from '%s' to '%s': %w", src, dst, err)
	}

	return nil
}

const VecDirName = ".vec"

// FindRepoRoot searches for the root directory of the Vec repository.
func FindRepoRoot() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}

	for {
		// Check if .vec directory exists in the current directory
		if _, err := os.Stat(filepath.Join(currentDir, VecDirName)); err == nil {
			return currentDir, nil // Found .vec directory
		}

		// Move to the parent directory
		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			return "", fmt.Errorf("not a vec repository (or any of the parent directories)")
		}
		currentDir = parentDir
	}
}

func HashFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file '%s': %w", filePath, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to calculate hash for '%s': %w", filePath, err)
	}
	hashString := fmt.Sprintf("%x", hash.Sum(nil))
	return hashString, nil
}
